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

func (self *Share) prepareFilter(f string) string {
	f = strings.TrimSpace(f)
	f = strings.TrimPrefix(f, filter.CriteriaSeparator)
	f = strings.TrimSuffix(f, filter.CriteriaSeparator)

	return f
}
