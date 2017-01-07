package db

import (
	"github.com/ghetzel/pivot/dal"
)

var MetadataSchema = dal.NewCollection(`metadata`).
	AddFields([]dal.Field{
		{
			Name: `name`,
			Type: dal.StringType,
		}, {
			Name: `parent`,
			Type: dal.StringType,
		}, {
			Name: `label`,
			Type: dal.StringType,
		}, {
			Name: `directory`,
			Type: dal.BooleanType,
		}, {
			Name: `last_modified_at`,
			Type: dal.IntType,
		}, {
			Name: `metadata`,
			Type: dal.ObjectType,
		},
	}...)

var SharesSchema = dal.NewCollection(`shares`).
	AddFields([]dal.Field{
		{
			Name: `name`,
			Type: dal.StringType,
		}, {
			Name: `parent`,
			Type: dal.StringType,
		}, {
			Name: `label`,
			Type: dal.StringType,
		}, {
			Name: `directory`,
			Type: dal.BooleanType,
		}, {
			Name: `last_modified_at`,
			Type: dal.IntType,
		}, {
			Name: `metadata`,
			Type: dal.ObjectType,
		},
	}...)

var SystemSchema = dal.NewCollection(`system`).
	AddFields([]dal.Field{
		{
			Name: `key`,
			Type: dal.StringType,
		}, {
			Name: `value`,
			Type: dal.ObjectType,
		},
	}...)
