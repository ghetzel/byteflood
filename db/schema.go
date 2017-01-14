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
