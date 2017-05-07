package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/pathutil"
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
				if err := api.Scan(c.Bool(`deep`), c.Args()...); err != nil {
					log.Fatalf("Failed to scan: %v", err)
				}
			},
		}, {
			Name:  `cleanup`,
			Usage: `Cleanup the metadata database.`,
			Action: func(c *cli.Context) {
				if err := api.Cleanup(); err != nil {
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
		}, {
			Name:      `export`,
			ArgsUsage: `[SCHEMA ..]`,
			Usage:     `Export one or more database collections to files.`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `directory, d`,
					Usage: `The directory to write the files to.`,
				},
				cli.BoolFlag{
					Name:  `all, a`,
					Usage: `Export all schemas.`,
				},
				cli.BoolFlag{
					Name:  `ignore-errors, I`,
					Usage: `Ignore (but still report) errors encountered during the export.`,
				},
				cli.BoolTFlag{
					Name:  `link-latest, L`,
					Usage: `Whether to automatically create or update a symbolic link to this export.`,
				},
				cli.StringFlag{
					Name:  `link-latest-name`,
					Usage: `The name of the "latest" symlink to create upon successful export.`,
					Value: `latest`,
				},
			},
			Action: func(c *cli.Context) {
				names := maputil.StringKeys(db.Models)
				sort.Strings(names)

				if c.NArg() == 0 && !c.Bool(`all`) {
					log.Fatalf("Must specify one or more schemas to dump: %s", strings.Join(names, `, `))
				} else {
					var exportDir string
					var exportList []string

					if c.Bool(`all`) {
						exportList = names
					} else {
						exportList = c.Args()
					}

					// make destination directory
					if v := c.String(`directory`); v != `` {
						exportDir = v
					} else {
						exportDir = path.Join(application.Database.BaseDirectory, `exports`, time.Now().Format(time.RFC3339))
					}

					if err := os.MkdirAll(exportDir, 0700); err != nil {
						log.Fatalf("Failed to create export directory: %v", err)
					}

					// perform exports
					for _, name := range exportList {
						if schema, ok := db.Schema[name]; ok && schema != nil {
							if model, ok := db.Models[name]; ok && model != nil {
								log.Infof("Exporting schema %q", name)

								recordset := dal.NewRecordSet()
								recordset.Options[`collection`] = schema

								if err := model.Each(&dal.Record{}, func(record interface{}, err error) {
									if err == nil {
										recordset.Push(record.(*dal.Record))
									} else {
										errAndMaybeQuit(c, "Error reading record in %q: %v", name, err)
									}
								}); err == nil {
									if len(recordset.Records) > 0 {
										exportFile := path.Join(exportDir, fmt.Sprintf("%s.json", name))

										if file, err := os.Create(exportFile); err == nil {
											if err := json.NewEncoder(file).Encode(recordset); err == nil {
												log.Noticef("Successfully exported schema %q to %v", name, exportFile)
											} else {
												errAndMaybeQuit(c, "Failed to export %v: %v", name, err)
											}
										} else {
											errAndMaybeQuit(c, "Failed to export %v: %v", name, err)
										}
									}
								} else {
									errAndMaybeQuit(c, "Error iterating over schema %q: %v", name, err)
								}
							} else {
								errAndMaybeQuit(c, "No such model %q", name)
							}
						} else {
							errAndMaybeQuit(c, "No such model %q", name)
						}
					}

					// move/create symlinks
					if c.Bool(`link-latest`) {
						linkDir := path.Join(path.Dir(exportDir), c.String(`link-latest-name`))

						if stat, err := os.Lstat(linkDir); err == nil {
							if pathutil.IsSymlink(stat.Mode()) {
								// remove existing symlink
								if err := os.Remove(linkDir); err != nil {
									log.Fatalf("Failed to remove %v: %v", linkDir, err)
								}
							} else {
								log.Fatalf("Path %v exists, but is not a symlink", linkDir)
							}
						}

						if err := os.Symlink(exportDir, linkDir); err == nil {
							log.Infof("Symbolic link created from %v -> %v", linkDir, exportDir)
						} else {
							log.Fatalf("Failed to create symlink to the latest export: %v", err)
						}
					}
				}
			},
		}, {
			Name:      `import`,
			ArgsUsage: `[SCHEMA ..]`,
			Usage:     `Import one or more database collections to files.`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `directory, d`,
					Usage: `The directory to write the files to.`,
				},
				cli.BoolFlag{
					Name:  `all, a`,
					Usage: `Export all schemas.`,
				},
				cli.BoolFlag{
					Name:  `ignore-errors, I`,
					Usage: `Ignore (but still report) errors encountered during the export.`,
				},
			},
			Action: func(c *cli.Context) {

			},
		},
	}
}
