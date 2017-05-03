package db

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/go-stockutil/typeutil"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/filter"
	"github.com/sabhiram/go-gitignore"
)

var MetadataSchema = &dal.Collection{
	Name:              `metadata`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `parent`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:      `size`,
			Type:      dal.IntType,
			Validator: dal.ValidatePositiveOrZeroInteger,
		}, {
			Name: `checksum`,
			Type: dal.StringType,
		}, {
			Name:     `label`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `directory`,
			Type:     dal.BooleanType,
			Required: true,
		}, {
			Name:      `children`,
			Type:      dal.IntType,
			Validator: dal.ValidatePositiveOrZeroInteger,
		}, {
			Name:      `descendants`,
			Type:      dal.IntType,
			Validator: dal.ValidatePositiveOrZeroInteger,
		}, {
			Name:     `last_modified_at`,
			Type:     dal.IntType,
			Required: true,
		}, {
			Name: `metadata`,
			Type: dal.ObjectType,
		},
	},
}

var SharesSchema = &dal.Collection{
	Name:              `shares`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:         `icon_name`,
			Type:         dal.StringType,
			Required:     true,
			DefaultValue: `folder`,
		},
		{
			Name: `filter`,
			Type: dal.StringType,
			Validator: func(value interface{}) error {
				if !typeutil.IsZero(value) {
					_, err := filter.Parse(fmt.Sprintf("%v", value))
					return err
				}

				return nil
			},
		}, {
			Name: `description`,
			Type: dal.StringType,
		}, {
			Name: `long_description`,
			Type: dal.StringType,
		}, {
			Name: `filter_templates`,
			Type: dal.StringType,
		}, {
			Name: `blocklist`,
			Type: dal.StringType,
		}, {
			Name: `allowlist`,
			Type: dal.StringType,
		},
	},
}

var DownloadsSchema = &dal.Collection{
	Name: `downloads`,
	Fields: []dal.Field{
		{
			Name:     `status`,
			Type:     dal.StringType,
			Required: true,
			Validator: dal.ValidateIsOneOf(
				`idle`,
				`pending`,
				`waiting`,
				`downloading`,
				`completed`,
				`failed`,
			),
		}, {
			Name: `priority`,
			Type: dal.IntType,
		}, {
			Name:     `peer_name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `session_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `share_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `file_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `destination`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `size`,
			Type:     dal.IntType,
			Required: true,
		}, {
			Name:     `added_at`,
			Type:     dal.TimeType,
			Required: true,
			Formatter: func(value interface{}, op dal.FieldOperation) (interface{}, error) {
				if op == dal.PersistOperation && typeutil.IsZero(value) {
					return time.Now(), nil
				}

				return value, nil
			},
		}, {
			Name: `error`,
			Type: dal.StringType,
		},
	},
}

// keyed on public peer ID
var AuthorizedPeersSchema = &dal.Collection{
	Name:              `authorized_peers`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
			Unique:   true,
			Formatter: func(value interface{}, op dal.FieldOperation) (interface{}, error) {
				return stringutil.Underscore(
					strings.TrimSpace(
						fmt.Sprintf("%v", value),
					),
				), nil
			},
		}, {
			Name: `groups`,
			Type: dal.StringType,
			Formatter: func(value interface{}, op dal.FieldOperation) (interface{}, error) {
				groupset := util.SplitMulti.Split(
					strings.TrimSpace(fmt.Sprintf("%v", value)),
					-1,
				)

				for i, group := range groupset {
					groupset[i] = `@` + stringutil.Underscore(strings.TrimPrefix(group, `@`))
				}

				return strings.Join(groupset, ` `), nil
			},
		}, {
			Name: `addresses`,
			Type: dal.StringType,
		},
	},
}

var SystemSchema = &dal.Collection{
	Name:              `system`,
	IdentityField:     `key`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name: `value`,
			Type: dal.ObjectType,
		},
	},
}

var ScannedDirectoriesSchema = &dal.Collection{
	Name:              `scanned_directories`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:        `path`,
			Description: `A local filesystem path that will be scanned for files.`,
			Type:        dal.StringType,
			Formatter: func(value interface{}, op dal.FieldOperation) (interface{}, error) {
				if expanded, err := pathutil.ExpandUser(fmt.Sprintf("%v", value)); err == nil {
					return expanded, nil
				} else {
					return ``, err
				}
			},
			Validator: func(value interface{}) error {
				dir := fmt.Sprintf("%v", value)

				if s, err := os.Stat(dir); err == nil {
					if !s.IsDir() {
						return fmt.Errorf("Given path must be a directory")
					}
				} else {
					return err
				}

				return nil
			},
		}, {
			Name:        `file_pattern`,
			Description: `A gitignore-style set of rules that specifies which files to exclude from scans.`,
			Type:        dal.StringType,
			Formatter: func(value interface{}, op dal.FieldOperation) (interface{}, error) {
				if !typeutil.IsZero(value) {
					return strings.TrimSpace(fmt.Sprintf("%v", value)), nil
				} else {
					return ``, nil
				}
			},
			Validator: func(value interface{}) error {
				_, err := ignore.CompileIgnoreLines(strings.Split(fmt.Sprintf("%v", value), "\n")...)
				return err
			},
		}, {
			Name:         `recursive`,
			Description:  `Whether to recursively scan subdirectories under this root directory.`,
			Type:         dal.BooleanType,
			Required:     true,
			DefaultValue: true,
		}, {
			Name:        `min_file_size`,
			Description: `If set, files smaller than this value will be skipped.`,
			Type:        dal.IntType,
			Validator:   dal.ValidatePositiveOrZeroInteger,
		}, {
			Name: `follow_symlinks`,
			Description: `If true, symbolic links in this directory will be followed when scanning, and the files ` +
				`therein will be imported as though they were in this directory.`,
			Type:         dal.BooleanType,
			Required:     true,
			DefaultValue: false,
		},
	},
}

var SubscriptionsSchema = &dal.Collection{
	Name: `subscriptions`,
	Fields: []dal.Field{
		{
			Name:        `source_group`,
			Description: `The peer name or @-prefixed peer group to source data from.`,
			Type:        dal.StringType,
			Required:    true,
		}, {
			Name:        `share_name`,
			Description: `Name of the remote share to query for data.`,
			Type:        dal.StringType,
			Required:    true,
		}, {
			Name:        `target_path`,
			Description: `The local filesystem path where downloaded data will be stored.`,
			Type:        dal.StringType,
			Required:    true,
			Formatter: func(value interface{}, op dal.FieldOperation) (interface{}, error) {
				if expanded, err := pathutil.ExpandUser(fmt.Sprintf("%v", value)); err == nil {
					return expanded, nil
				} else {
					return ``, err
				}
			},
			Validator: func(value interface{}) error {
				dir := fmt.Sprintf("%v", value)

				if s, err := os.Stat(dir); err == nil {
					if !s.IsDir() {
						return fmt.Errorf("Given path must be a directory")
					}
				} else if os.IsNotExist(err) {
					if err := os.MkdirAll(dir, 0755); err != nil {
						return err
					}
				} else {
					return err
				}

				return nil
			},
		}, {
			Name:        `sync_interval`,
			Description: `A cron-like specification for declaring how often (if ever) the subscription should periodically sync.`,
			Type:        dal.StringType,
		}, {
			Name:        `filter`,
			Description: `A filter used to narrow down which files to monitor from the named share.`,
			Type:        dal.StringType,
		}, {
			Name:        `bytes_downloaded`,
			Description: `How much data has been downloaded within the current quota interval.`,
			Type:        dal.IntType,
		}, {
			Name:        `quota`,
			Description: `The maximum number of bytes to download within a given span of time.`,
			Type:        dal.IntType,
		}, {
			Name:        `quota_reset_at`,
			Description: `The last time the quota was reset.`,
			Type:        dal.TimeType,
		}, {
			Name:        `quota_interval`,
			Description: `The amount of time (in seconds) that a quota applies to.`,
			Type:        dal.IntType,
		},
	},
}
