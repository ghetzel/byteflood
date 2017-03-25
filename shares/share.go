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
	ID              string `json:"id"`
	IconName        string `json:"icon_name"`
	BaseFilter      string `json:"filter,omitempty"`
	Description     string `json:"description,omitempty"`
	LongDescription string `json:"long_description,omitempty"`
	db              *db.Database
}

func NewShare(conn *db.Database) *Share {
	icon := ``

	if field, ok := db.SharesSchema.GetField(`icon_name`); ok {
		icon = fmt.Sprintf("%v", field.DefaultValue)
	}

	return &Share{
		IconName: icon,
		db:       conn,
	}
}

func (self *Share) GetQuery(filters ...string) string {
	if self.BaseFilter == `` {
		self.BaseFilter = fmt.Sprintf("label%s%s", filter.FieldTermSeparator, stringutil.Underscore(self.ID))
	}

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
		if err := self.db.Metadata.Find(f, &entries); err == nil {
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

		if len(sort) == 0 {
			f.Sort = []string{`-directory`, `name`}
		} else {
			f.Sort = sort
		}

		var recordset dal.RecordSet

		if err := self.db.Metadata.Find(f, &recordset); err == nil {
			return &recordset, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *Share) Get(id string) (*db.File, error) {
	entry := new(db.File)

	if err := self.db.Metadata.Get(id, entry); err == nil {
		entry.SetDatabase(self.db)
		return entry, nil
	} else {
		return nil, err
	}
}

func (self *Share) Children(filterString ...string) ([]*db.File, error) {
	if f, err := db.ParseFilter(self.GetQuery(append(filterString, `parent=root`)...)); err == nil {
		f.Limit = db.MaxChildEntries
		f.Sort = []string{`-directory`, `name`}

		files := make([]*db.File, 0)

		if err := self.db.Metadata.Find(f, &files); err == nil {
			for _, file := range files {
				file.SetDatabase(self.db)
			}

			return files, nil
		} else {
			return nil, err
		}
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
