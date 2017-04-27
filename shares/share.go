package shares

import (
	"fmt"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/stats"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/mobius"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/filter"
	"github.com/op/go-logging"
	"strings"
	"time"
)

var log = logging.MustGetLogger(`byteflood/shares`)

type Stats struct {
	FileCount      uint64 `json:"file_count"`
	DirectoryCount uint64 `json:"directory_count"`
	TotalBytes     uint64 `json:"total_bytes"`
}

type Share struct {
	ID              string `json:"id"`
	IconName        string `json:"icon_name"`
	BaseFilter      string `json:"filter,omitempty"`
	Description     string `json:"description,omitempty"`
	LongDescription string `json:"long_description,omitempty"`
	Blocklist       string `json:"blocklist,omitempty"`
	Allowlist       string `json:"allowlist,omitempty"`
	Stats           *Stats `json:"stats,omitempty"`
	db              *db.Database
}

func GetShares(conn *db.Database, requestingPeer peer.Peer, stats bool, ids ...string) ([]Share, error) {
	var shares []Share
	output := make([]Share, 0)

	if err := conn.Shares.All(&shares); err == nil {
		for _, share := range shares {
			if len(ids) == 0 || sliceutil.ContainsString(ids, share.ID) {
				if share.IsPeerPermitted(requestingPeer) {
					if stats {
						if s, err := share.GetStats(); err == nil {
							share.Stats = s
						} else {
							return nil, err
						}
					}

					// remote peers should not be able to see the Allow/Blocklists
					if !requestingPeer.IsLocal() {
						share.Allowlist = ``
						share.Blocklist = ``
					}

					output = append(output, share)
				}
			}
		}

		if len(ids) > 0 && len(output) != len(ids) {
			return nil, fmt.Errorf("Incorrect number of shares retrieved")
		}

		return output, nil
	} else {
		return nil, err
	}
}

func (self *Share) SetDatabase(conn *db.Database) {
	self.db = conn
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

func (self *Share) IsPeerPermitted(p peer.Peer) bool {
	// we're always allowed to see our own shares
	if p.IsLocal() {
		return true
	}

	blocks := util.SplitMulti.Split(self.Blocklist, -1)
	allows := util.SplitMulti.Split(self.Allowlist, -1)

	if ap, err := peer.GetAuthorizedPeer(self.db, p.GetID()); err == nil {
		permit := true

		for _, block := range blocks {
			switch block {
			case `*`:
				// if a wildcard block is given, peers or peer groups explicitly named in
				// the allowlist can still override
				permit = false
				break

			default:
				// if a peer or peer group is explicitly named in the blocklist, then
				// that takes precedence and we return false immediately
				if ap.IsMemberOf(block) {
					return false
				}
			}
		}

		// only check the allow list if we're not already permitted
		if !permit {
			for _, allow := range allows {
				if ap.IsMemberOf(allow) {
					permit = true
					break
				}
			}
		}

		return permit
	} else {
		// if we can't find an authorized peer for the given ID, then deny by default
		return false
	}
}

func (self *Share) Length() int {
	if stats, err := self.GetStats(); err == nil {
		return int(stats.FileCount) + int(stats.DirectoryCount)
	} else {
		return 0
	}
}

func (self *Share) Find(filterString string, limit int, offset int, sort []string, fields []string) (*dal.RecordSet, error) {
	if f, err := db.ParseFilter(self.GetQuery(filterString)); err == nil {
		f.Limit = limit
		f.Offset = offset

		if len(sort) == 0 {
			f.Sort = []string{`-directory`, `name`}
		} else {
			f.Sort = sort
		}

		if len(fields) > 0 {
			f.Fields = fields
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

func (self *Share) List(field string, filterString string, limit int, offset int, sort []string) ([]interface{}, error) {
	if f, err := db.ParseFilter(self.GetQuery(filterString)); err == nil {
		f.Limit = limit
		f.Offset = offset

		if len(sort) == 0 {
			f.Sort = []string{`-directory`, `name`}
		} else {
			f.Sort = sort
		}

		if results, err := self.db.Metadata.ListWithFilter([]string{field}, f); err == nil {
			if v, ok := results[field]; ok {
				return v, nil
			} else {
				return []interface{}{}, nil
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (self *Share) Get(id string) (*db.Entry, error) {
	entry := db.NewEntry(self.db, ``, ``, ``)

	if err := self.db.Metadata.Get(id, entry); err == nil {
		entry.SetDatabase(self.db)

		return entry, nil
	} else {
		return nil, err
	}
}

func (self *Share) GetStats() (*Stats, error) {
	if metrics, err := stats.StatsDB.Range(
		time.Now().Add(-1*time.Hour),
		time.Now(),
		fmt.Sprintf("byteflood.shares.*:share=%s", self.ID),
	); err == nil {
		shareStats := &Stats{}

		for _, metric := range metrics {
			values := metric.Summarize(mobius.Last)

			switch metric.GetName() {
			case `byteflood.shares.total_bytes`:
				shareStats.TotalBytes = uint64(values[0])
			case `byteflood.shares.file_count`:
				shareStats.FileCount = uint64(values[0])
			case `byteflood.shares.directory_count`:
				shareStats.DirectoryCount = uint64(values[0])
			}
		}

		return shareStats, nil
	} else {
		return nil, err
	}
}

func (self *Share) RefreshShareStats() error {
	if f, err := db.ParseFilter(self.GetQuery()); err == nil {
		f.Limit = -1
		f.Fields = []string{`directory`, `size`}
		f.Sort = []string{`-directory`, `size`}

		// file stats
		if filesFilter, err := f.NewFromMap(map[string]interface{}{
			`bool:directory`: `false`,
		}); err == nil {
			if v, err := self.db.Metadata.Sum(`size`, filesFilter); err == nil {
				stats.Gauge(`byteflood.shares.total_bytes`, float64(v), map[string]interface{}{
					`share`: self.ID,
				})
			} else {
				return err
			}

			if v, err := self.db.Metadata.Count(filesFilter); err == nil {
				stats.Gauge(`byteflood.shares.file_count`, float64(v), map[string]interface{}{
					`share`: self.ID,
				})
			} else {
				return err
			}
		} else {
			return err
		}

		// directory stats
		if dirFilter, err := f.NewFromMap(map[string]interface{}{
			`bool:directory`: `true`,
		}); err == nil {
			if v, err := self.db.Metadata.Count(dirFilter); err == nil {
				stats.Gauge(`byteflood.shares.directory_count`, float64(v), map[string]interface{}{
					`share`: self.ID,
				})
			} else {
				return err
			}
		} else {
			return err
		}

		return nil
	} else {
		return err
	}
}

func (self *Share) Children(filterString ...string) ([]*db.Entry, error) {
	if f, err := db.ParseFilter(self.GetQuery(append(filterString, `parent=root`)...)); err == nil {
		f.Limit = db.MaxChildEntries
		f.Sort = []string{`-directory`, `name`}

		files := make([]*db.Entry, 0)

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
