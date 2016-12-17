package shares

import (
    "fmt"
    "github.com/op/go-logging"
    "github.com/ghodss/yaml"
    "github.com/ghetzel/byteflood/scanner"
    "io/ioutil"
    "io"
    "strings"
)

var log = logging.MustGetLogger(`byteflood/shares`)

type Share struct {
    Name string `json:"name"`
    BaseFilter string `json:"filter"`
    Description string `json:"description,omitempty"`
    FilterTemplates []string `json:"filter_templates,omitempty"`
    metabase *scanner.Scanner
}

func LoadShare(metabase *scanner.Scanner, reader io.Reader) (*Share, error) {
    if data, err := ioutil.ReadAll(reader); err == nil {
        share := NewShare(metabase, ``)

        if err := yaml.Unmarshal(data, share); err == nil {
            if share.Name == `` {
                return nil, fmt.Errorf("share must have a name")
            }

            if share.BaseFilter == `` {
                return nil, fmt.Errorf("share specify a base filter")
            }

            if share.metabase == nil {
                return nil, fmt.Errorf("share must be attached to a metadata database")
            }

            return share, nil

        } else {
            return nil, err
        }
    } else{
        return nil, err
    }
}

func NewShare(metabase *scanner.Scanner, baseFilter string) *Share {
    return &Share{
        BaseFilter: baseFilter,
        metabase: metabase,
    }
}

func (self *Share) GetQuery(filters ...string) string {
    for i, filter := range filters {
        filters[i] = self.prepareFilter(filter)
    }

    filters = append([]string{
        self.prepareFilter(self.BaseFilter),
    }, filters...)

    return strings.Join(filters, `/`)
}

func (self *Share) Length() int {
    if rs,err := self.metabase.QueryRecords(self.GetQuery()); err == nil {
        return int(rs.ResultCount)
    }else{
        return 0
    }
}

func (self *Share) FindFunc(filter string, recordFn func(map[string]interface{})) error {
    if rs,err := self.metabase.QueryRecords(self.GetQuery(filter)); err == nil {
        for _, record := range rs.Records {
            recordFn(record.Fields)
        }
    }else{
        return err
    }

    return nil
}

func (self *Share) Find(filter string) ([]map[string]interface{}, error) {
    results := make([]map[string]interface{}, 0)

    if err := self.FindFunc(filter, func(record map[string]interface{}) {
        results = append(results, record)
    }); err != nil {
        return nil, err
    }

    return results, nil
}


func (self *Share) prepareFilter(filter string) string {
    filter = strings.TrimSpace(filter)
    filter = strings.TrimPrefix(filter, `/`)
    filter = strings.TrimSuffix(filter, `/`)

    return filter
}
