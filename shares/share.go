package shares

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/filter"
	"github.com/op/go-logging"
	"strings"
)

var log = logging.MustGetLogger(`byteflood/shares`)

type Share struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	BaseFilter      string   `json:"filter,omitempty"`
	Description     string   `json:"description,omitempty"`
	FilterTemplates []string `json:"filter_templates,omitempty"`
}

func NewShare() *Share {
	return new(Share)
}

func (self *Share) Initialize() error {
	if self.Name == `` {
		return fmt.Errorf("share must have a name")
	}

	if self.BaseFilter == `` {
		self.BaseFilter = fmt.Sprintf("label%s%s", filter.FieldTermSeparator, stringutil.Underscore(self.Name))
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

	return strings.Join(sliceutil.CompactString(filters), filter.CriteriaSeparator)
}

func (self *Share) Length() int {
	var entries []*db.File

	if f, err := db.ParseFilter(self.GetQuery()); err == nil {
		if err := db.Metadata.Find(f, &entries); err == nil {
			return len(entries)
		} else {
			return 0
		}
	} else {
		return 0
	}
}

func (self *Share) Find(filterString string, limit int, offset int, sort []string) (*dal.RecordSet, error) {
	if f, err := db.ParseFilter(self.GetQuery(filterString)); err == nil {
		f.Limit = limit
		f.Offset = offset

		if sort == nil {
			f.Sort = []string{`-directory`, `name`}
		} else {
			f.Sort = sort
		}

		recordset := dal.NewRecordSet()

		if err := db.Metadata.Find(f, recordset); err == nil {
			return recordset, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *Share) Get(id string) (*db.File, error) {
	entry := new(db.File)

	if err := db.Metadata.Get(id, entry); err == nil {
		return entry, nil
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
