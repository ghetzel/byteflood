package db

import (
	"bufio"
	"fmt"
	"github.com/ghetzel/go-stockutil/stringutil"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ManifestItemType string

const (
	FileItem      ManifestItemType = `file`
	DirectoryItem                  = `directory`
)

type ManifestValue interface{}

var DefaultManifestFields = []string{ `id`, `relative_path`, `type` }

type ManifestItem struct {
	ID           string
	Type         ManifestItemType
	Label        string
	RelativePath string
	Values       []ManifestValue
}

func (self *ManifestItem) NeedsUpdate(manifest *Manifest, policy *SyncPolicy) (bool, error) {
	var absPath string
	var stat os.FileInfo

	// perform the local filesystem check.  If the file does not exist, then we don't have it
	if localAbsPath, err := filepath.Abs(
		filepath.Join(manifest.BaseDirectory, self.RelativePath),
	); err == nil {
		absPath = localAbsPath
		log.Debugf("Item %s: check existence at %s", self.ID, localAbsPath)

		if fileinfo, err := os.Stat(localAbsPath); os.IsNotExist(err) {
			return true, nil
		} else if err != nil {
			return false, err
		} else {
			stat = fileinfo
		}
	} else {
		return false, err
	}

	switch self.Type {
	case DirectoryItem:
		if stat.IsDir() {
			return false, nil
		}
	default:
		// perform field checks
		file := NewFile(nil, self.Label, manifest.BaseDirectory, absPath)

		for i, value := range self.Values {
			if i < len(manifest.Fields) {
				fieldName := manifest.Fields[i]
				log.Debugf("Item %s: check field %q", self.ID, fieldName)

				switch fieldName {
				case `checksum`:
					if sum, err := file.GenerateChecksum(); err == nil {
						if fmt.Sprintf("%v", value) != sum {
							log.Debugf("  field %s no match %v", `checksum`, sum)
							return true, nil
						}
					} else {
						return false, err
					}

				default:
					// lazy load file metadata
					if !file.metadataLoaded {
						if err := file.LoadMetadata(); err != nil {
							return false, err
						}
					}

					// perform metadata comparison
					if !policy.Compare(fieldName, file.Get(fieldName), value) {
						log.Debugf("  field %s no match %v", fieldName, value)
						return true, nil
					}
				}
			} else {
				return false, fmt.Errorf(
					"Manifest item %s contains fewer fields than the given policy requires",
					self.ID,
				)
			}
		}
	}

	return false, nil
}

type Manifest struct {
	BaseDirectory string
	Items         []ManifestItem
	Fields        []string
}

func NewManifest(baseDirectory string, fields ...string) *Manifest {
	return &Manifest{
		BaseDirectory: baseDirectory,
		Items:         make([]ManifestItem, 0),
		Fields:        fields,
	}
}

func (self *Manifest) LoadTSV(r io.Reader, fields ...string) error {
	scanner := bufio.NewScanner(r)
	self.Fields = nil
	headerSkipped := false

	fields = append(DefaultManifestFields, fields...)

	for scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}

		// skip the first line (header)
		if !headerSkipped {
			headerSkipped = true
			continue
		}

		line := scanner.Text()
		values := strings.Split(line, "\t")

		if len(values) != len(fields) {
			return fmt.Errorf(
				"Column count does not match given schema (got %d values for %d fields)",
				len(values),
				len(fields),
			)
		}

		item := ManifestItem{}

		for i, value := range values {
			fieldName := fields[i]

			switch fieldName {
			case `id`:
				item.ID = value
			case `type`:
				switch value {
				case `directory`:
					item.Type = DirectoryItem
				case `file`:
					item.Type = FileItem
				default:
					return fmt.Errorf("Unrecognized type %q", value)
				}
			case `label`:
				item.Label = value
			case `relative_path`:
				item.RelativePath = value
			default:
				self.Fields = append(self.Fields, fieldName)

				if value == `` {
					item.Values = append(item.Values, nil)
				} else {
					item.Values = append(item.Values, stringutil.Autotype(value))
				}
			}
		}

		self.Add(item)
	}

	return nil
}

func (self *Manifest) Add(items ...ManifestItem) {
	self.Items = append(self.Items, items...)
}

func (self *Manifest) GetUpdateManifest(policy SyncPolicy) (*Manifest, error) {
	diff := NewManifest(self.BaseDirectory)
	copy(diff.Fields, self.Fields)

	for _, item := range self.Items {
		if update, err := item.NeedsUpdate(self, &policy); err == nil {
			if update {
				diff.Add(item)
			}
		} else {
			return nil, err
		}
	}

	return diff, nil
}
