package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/pivot/dal"
)

func subcommandsDB() []cli.Command {
	return []cli.Command{
		{
			Name:  `scan`,
			Usage: `Scans all configured source directories for changes`,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  `deep, D`,
					Usage: `Force a deep scan of all file metadata regardless of age.`,
				},
			},
			Action: func(c *cli.Context) {
				if err := application.Scan(c.Bool(`deep`), c.Args()...); err != nil {
					log.Fatalf("Failed to scan: %v", err)
				}

			},
		}, {
			Name:  `cleanup`,
			Usage: `Cleanup the metadata database.`,
			Action: func(c *cli.Context) {
				if err := application.Database.Cleanup(); err != nil {
					log.Fatal(err)
				}
			},
		}, {
			Name:      `query`,
			Usage:     `Query the metadata database.`,
			ArgsUsage: `FILTER`,
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:  `field, F`,
					Usage: `Additional fields to include in output tables in addition to ID and path`,
				},
				cli.StringFlag{
					Name:  `db`,
					Usage: `Query the named database`,
					Value: db.MetadataSchema.Name,
				},
			},
			Action: func(c *cli.Context) {
				if f, err := db.ParseFilter(strings.Join(c.Args(), `/`)); err == nil {
					var rs dal.RecordSet

					if err := db.Metadata.Find(f, &rs); err == nil {
						printWithFormat(c.GlobalString(`format`), rs, func() {
							tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)

							for _, record := range rs.Records {
								values := make([]interface{}, 0)

								values = append(values, record.ID)

								for _, fieldName := range c.StringSlice(`field`) {
									if v := maputil.DeepGet(record.Fields, strings.Split(fieldName, `.`), nil); v != nil {
										values = append(values, v)
									}
								}

								fmt.Fprintf(tw, strings.TrimSpace(strings.Repeat("%v\t", len(values)))+"\n", values...)
							}

							tw.Flush()
						})
					} else {
						log.Fatal(err)
					}
				} else {
					log.Fatal(err)
				}
			},
		},
	}
}
