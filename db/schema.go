package db

import (
	"github.com/ghetzel/pivot/dal"
)

var MetadataSchema = dal.Collection{
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
			Name:     `label`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `directory`,
			Type:     dal.BooleanType,
			Required: true,
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

var SharesSchema = dal.Collection{
	Name: `shares`,
	Fields: []dal.Field{
		{
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
			Unique:   true,
		}, {
			Name: `filter`,
			Type: dal.StringType,
		}, {
			Name: `description`,
			Type: dal.StringType,
		}, {
			Name: `filter_templates`,
			Type: dal.StringType,
		},
	},
}

var DownloadsSchema = dal.Collection{
	Name: `downloads`,
	Fields: []dal.Field{
		{
			Name:     `status`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name: `priority`,
			Type: dal.IntType,
		}, {
			Name:     `session_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `file_id`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name:     `filename`,
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
			Name:     `peer_name`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name: `error`,
			Type: dal.StringType,
		}, {
			Name:     `added_at`,
			Type:     dal.TimeType,
			Required: true,
		},
	},
}

// keyed on public peer ID
var AuthorizedPeersSchema = dal.Collection{
	Name:              `authorized_peers`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:     `name`,
			Type:     dal.StringType,
			Required: true,
		},
	},
}

var SystemSchema = dal.Collection{
	Name:              `system`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:     `key`,
			Type:     dal.StringType,
			Identity: true,
		}, {
			Name: `value`,
			Type: dal.ObjectType,
		},
	},
}

var ScannerSchema = dal.Collection{
	Name:              `scanned_directories`,
	IdentityFieldType: dal.StringType,
	Fields: []dal.Field{
		{
			Name:     `path`,
			Type:     dal.StringType,
			Identity: true,
		}, {
			Name: `parent`,
			Type: dal.StringType,
		}, {
			Name:     `label`,
			Type:     dal.StringType,
			Required: true,
		}, {
			Name: `file_pattern`,
			Type: dal.StringType,
		}, {
			Name:         `recursive`,
			Type:         dal.BooleanType,
			Required:     true,
			DefaultValue: true,
		}, {
			Name:     `min_file_size`,
			Type:     dal.IntType,
			Required: true,
		}, {
			Name: `checksum`,
			Type: dal.BooleanType,
		},
	},
}
