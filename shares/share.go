package shares

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/filter"
	"github.com/ghodss/yaml"
	"github.com/op/go-logging"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

var log = logging.MustGetLogger(`byteflood/shares`)

type Share struct {
	Name            string   `json:"name"`
	BaseFilter      string   `json:"filter,omitempty"`
	Description     string   `json:"description,omitempty"`
	FilterTemplates []string `json:"filter_templates,omitempty"`
	metabase        *db.Database
}

func LoadShare(metabase *db.Database, reader io.Reader) (*Share, error) {
	if data, err := ioutil.ReadAll(reader); err == nil {
		share := NewShare()
		share.SetMetabase(metabase)

		if err := yaml.Unmarshal(data, share); err == nil {
			if err := share.Initialize(); err == nil {
				return share, nil
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func NewShare() *Share {
	return &Share{}
}

func (self *Share) SetMetabase(metabase *db.Database) {
	self.metabase = metabase
}

func (self *Share) Initialize() error {
	if self.Name == `` {
		return fmt.Errorf("share must have a name")
	}

	if self.BaseFilter == `` {
		self.BaseFilter = fmt.Sprintf("label%s%s", filter.FieldTermSeparator, stringutil.Underscore(self.Name))
	}

	if self.metabase == nil {
		return fmt.Errorf("share must be attached to a metadata database")
	}

	return nil
}

func (self *Share) GetQuery(filters ...string) string {
	for i, f := range filters {
		filters[i] = self.prepareFilter(f)
	}

	filters = append([]string{
		self.prepareFilter(self.BaseFilter),
	}, filters...)

	return strings.Join(filters, filter.CriteriaSeparator)
}

func (self *Share) Length() int {
	if rs, err := self.metabase.QueryMetadata(self.GetQuery()); err == nil {
		return int(rs.ResultCount)
	} else {
		return 0
	}
}

func (self *Share) FindFunc(f string, recordFn func(*dal.Record)) error {
	if rs, err := self.metabase.QueryMetadata(self.GetQuery(f)); err == nil {
		for _, record := range rs.Records {
			recordFn(record)
		}
	} else {
		return err
	}

	return nil
}

func (self *Share) Find(filterString string, limit int, offset int, sort []string) (*dal.RecordSet, error) {
	if f, err := self.metabase.ParseFilter(self.GetQuery(filterString)); err == nil {
		f.Limit = limit
		f.Offset = offset

		if sort == nil {
			f.Sort = []string{`-directory`, `name`}
		} else {
			f.Sort = sort
		}

		return self.metabase.Query(self.metabase.MetadataCollectionName, f)
	} else {
		return nil, err
	}
}

func (self *Share) Get(id string) (*dal.Record, error) {
	if f, err := self.metabase.ParseFilter(self.GetQuery(fmt.Sprintf("_id=is:%s", id))); err == nil {
		if recordset, err := self.metabase.Query(self.metabase.MetadataCollectionName, f); err == nil {
			switch len(recordset.Records) {
			case 1:
				if recordset.Records[0].ID == id {
					return recordset.Records[0], nil
				} else {
					return nil, fmt.Errorf("wrong record returned")
				}
			case 0:
				return nil, fmt.Errorf("not found")
			default:
				return nil, fmt.Errorf("too many results")
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *Share) GetFilePath(id string) (string, error) {
	if record, err := self.Get(id); err == nil {
		if v := self.metabase.PropertyGet(fmt.Sprintf("metadata.paths.%s", record.ID)); v != nil {
			if absPath, ok := v.(string); ok {
				if _, err := os.Stat(absPath); err == nil {
					return absPath, nil
				}
			}
		}

		return ``, fmt.Errorf("invalid entry")
	} else {
		return ``, err
	}
}

func (self *Share) prepareFilter(f string) string {
	f = strings.TrimSpace(f)
	f = strings.TrimPrefix(f, filter.CriteriaSeparator)
	f = strings.TrimSuffix(f, filter.CriteriaSeparator)

	return f
}
