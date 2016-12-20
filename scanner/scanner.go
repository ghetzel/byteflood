package scanner

import (
	"fmt"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot"
	"github.com/ghetzel/pivot/backends"
	"github.com/ghetzel/pivot/dal"
	"github.com/op/go-logging"
	"time"
)

var log = logging.MustGetLogger(`byteflood/scanner`)

var DefaultCollectionName = `metadata`
var DefaultManagementCollectionName = `byteflood`

type ScanOptions struct {
	FilePattern          string `json:"patterns,omitempty"`
	NoRecurseDirectories bool   `json:"no_recurse,omitempty"`
	FileMinimumSize      int    `json:"file_min_size,omitempty"`
	QuickScan            bool   `json:"quick_scan"`
}

func DefaultScanOptions() ScanOptions {
	return ScanOptions{
		NoRecurseDirectories: false,
	}
}

type Scanner struct {
	Directories          []*Directory
	Options              ScanOptions
	DatabaseURI          string
	Collection           string
	ManagementCollection string
	db                   backends.Backend
}

func NewScanner(options *ScanOptions) *Scanner {
	var scannerOptions ScanOptions

	if options == nil {
		scannerOptions = DefaultScanOptions()
	} else {
		scannerOptions = *options
	}

	return &Scanner{
		Directories:          make([]*Directory, 0),
		Options:              scannerOptions,
		DatabaseURI:          `boltdb:///~/.local/share/byteflood/db`,
		Collection:           DefaultCollectionName,
		ManagementCollection: DefaultManagementCollectionName,
	}
}

// Initialize the Scanner by opening the underlying database
func (self *Scanner) Initialize() error {
	if db, err := pivot.NewDatabase(self.DatabaseURI); err == nil {
		self.db = db
	} else {
		return err
	}

	return nil
}

// Add a directory to be scanned
func (self *Scanner) AddDirectory(directory *Directory) error {
	if err := directory.Initialize(self); err == nil {
		self.Directories = append(self.Directories, directory)
		return nil
	} else {
		return err
	}
}

// Query records in the given collection
func (self *Scanner) QueryRecordsFromCollection(collectionName string, filterString string) (*dal.RecordSet, error) {
	if index := self.db.WithSearch(); index != nil {
		return index.QueryString(collectionName, filterString)
	} else {
		return nil, fmt.Errorf("Backend type %T does not support searching", self.db)
	}
}

// Return whethere a metadata record with the given ID exists
func (self *Scanner) RecordExists(id string) bool {
	return self.db.Exists(self.Collection, id)
}

// Retrieve a metadata record by ID
func (self *Scanner) RetrieveRecord(id string) (map[string]interface{}, error) {
	if record, err := self.db.Retrieve(self.Collection, id); err == nil {
		return record.Fields, nil
	} else {
		return nil, err
	}
}

// Save a given metadata record
func (self *Scanner) PersistRecord(id string, data map[string]interface{}) error {
	return self.db.Insert(self.Collection, dal.NewRecordSet(
		dal.NewRecord(id).SetFields(data),
	))
}

// Delete metadata records that match the given set of IDs
func (self *Scanner) DeleteRecords(ids ...string) error {
	return self.db.Delete(self.Collection, ids)
}

// Query records from the metadata collection
func (self *Scanner) QueryRecords(filterString string) (*dal.RecordSet, error) {
	return self.QueryRecordsFromCollection(self.Collection, filterString)
}

// Removes records from the database that would not be added by the current Scanner instance.
func (self *Scanner) CleanRecords() error {
	var minLastSeen int64

	if v := self.PropertyGet(`metadata.last_scan`); v != nil {
		if vI, ok := v.(int64); ok {
			minLastSeen = vI
		}
	}

	if minLastSeen <= 0 {
		return fmt.Errorf("Invalid last_scan time")
	}

	if staleRecordSet, err := self.QueryRecordsFromCollection(
		self.ManagementCollection,
		fmt.Sprintf("key/prefix:metadata.last_scan./value/lt:%d", minLastSeen),
	); err == nil {
		ids := make([]string, 0)

		for _, record := range staleRecordSet.Records {
			if v := record.Get(`value`); v != nil {
				if vStr, err := stringutil.ToString(v); err == nil {
					ids = append(ids, vStr)
				}
			}
		}

		log.Debugf("Cleaning up %d records", len(ids))

		return self.DeleteRecords(ids...)
	} else {
		return err
	}
}

func (self *Scanner) PropertySet(key string, value interface{}, fields ...map[string]interface{}) error {
	record := dal.NewRecord(key).Set(`key`, key).Set(`value`, value)

	if len(fields) > 0 {
		record.SetFields(fields[0])
	}

	return self.db.Insert(self.ManagementCollection, dal.NewRecordSet(record))
}

func (self *Scanner) PropertyGet(key string, fallback ...interface{}) interface{} {
	if record, err := self.db.Retrieve(self.ManagementCollection, key); err == nil {
		return record.Get(`value`, fallback...)
	} else {
		if len(fallback) > 0 {
			return fallback[0]
		} else {
			return nil
		}
	}
}

func (self *Scanner) Scan() error {
	// get this before performing the scan so that all scanned files will necessarily
	// be greater than it
	minLastSeen := time.Now().UnixNano()

	for _, directory := range self.Directories {
		log.Debugf("Scanning directory %s [%s]", directory.Path, directory.Label)
		if err := directory.Scan(); err != nil {
			return err
		}
	}

	return self.PropertySet(`metadata.last_scan`, minLastSeen)
}
