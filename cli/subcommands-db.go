package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/metabase"
	"github.com/ghetzel/pivot/dal"
)

func subcommandsDB() []cli.Command {
	return []cli.Command{
		{
			Name:  `db`,
			Usage: `Manage metadata database and access.`,
			Subcommands: []cli.Command{
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
							Value: metabase.MetadataSchema.Name,
						},
					},
					Action: func(c *cli.Context) {
						if f, err := metabase.ParseFilter(strings.Join(c.Args(), `/`)); err == nil {
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
								exportDir = path.Join(
									application.Database.BaseDirectory,
									`exports`,
									strings.Replace(
										time.Now().Format(time.RFC3339),
										`:`,
										`-`,
										-1,
									),
								)
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

										if err := model.Each(&dal.Record{}, func(record interface{}, err error) {
											if err == nil {
												recordset.Push(record.(*dal.Record))
											} else {
												errAndMaybeQuit(c, "Error reading record in %q: %v", name, err)
											}
										}); err == nil {
											exportFile := path.Join(exportDir, fmt.Sprintf("%s.bak", name))

											// output pipeline:
											//   json encode -> gzip -> cryptobox -> file
											if file, err := os.Create(exportFile); err == nil {
												// encryption writes to the output file
												encrypter := encryption.NewCryptoboxEncryption(
													application.LocalPeer.PublicKey,
													application.LocalPeer.PrivateKey,
													nil,
													file,
												)

												// gzip writes to the encrypter
												gzipper := gzip.NewWriter(encrypter)
												defer gzipper.Close()

												// JSON: encode and write out schema descriptor
												if descriptor, err := json.Marshal(schema); err == nil {
													gzipper.Write(descriptor)
													gzipper.Write([]byte{'\n'})

													// JSON: encode and write out each record
													for _, record := range recordset.Records {
														if data, err := json.Marshal(record); err == nil {
															gzipper.Write(data)
															gzipper.Write([]byte{'\n'})
														} else {
															errAndMaybeQuit(c, "Failed to export record %v[%v]: %v", name, record.ID, err)
														}
													}
												} else {
													errAndMaybeQuit(c, "Failed to export %v: %v", name, err)
												}

												gzipper.Close()
												log.Noticef("Successfully exported schema %q to %v", name, exportFile)
											} else {
												errAndMaybeQuit(c, "Failed to export %v: %v", name, err)
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
					ArgsUsage: `[FILES ..]`,
					Usage:     `Import one or more database collections to files.`,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  `directory, d`,
							Usage: `The directory to read the files from.`,
						},
						cli.BoolFlag{
							Name:  `all, a`,
							Usage: `Import all schemas.`,
						},
						cli.BoolFlag{
							Name:  `force`,
							Usage: `Force the import, even if the schemas don't match up.`,
						},
						cli.BoolFlag{
							Name:  `ignore-errors, I`,
							Usage: `Ignore (but still report) errors encountered during the export.`,
						},
						cli.BoolFlag{
							Name:  `dump-only`,
							Usage: `Print the decrypted backup stream to standard output instead of restoring it to the database.`,
						},
					},
					Action: func(c *cli.Context) {
						if c.NArg() == 0 && !c.Bool(`all`) {
							log.Fatalf("Must specify one or more files to restore from")
						} else {
							var importDir string
							var importList []string

							// make destination directory
							if v := c.String(`directory`); v != `` {
								importDir = v
							} else {
								importDir = path.Join(application.Database.BaseDirectory, `exports`, `latest`)
							}

							if c.Bool(`all`) {
								if entries, err := ioutil.ReadDir(importDir); err == nil {
									for _, entry := range entries {
										if entry.Mode().IsRegular() {
											importList = append(importList, path.Join(importDir, entry.Name()))
										}
									}
								}
							} else {
								importList = c.Args()
							}

						ImportFile:
							for _, importName := range importList {
								if file, err := os.Open(importName); err == nil {
									// encryption reads from the input file
									decrypter := encryption.NewCryptoboxEncryption(
										application.LocalPeer.PublicKey,
										application.LocalPeer.PrivateKey,
										file,
										nil,
									)

									// gzip reads from the decrypter
									if gunzipper, err := gzip.NewReader(decrypter); err == nil {
										defer gunzipper.Close()

										// read the backup, line by line
										scanner := bufio.NewScanner(gunzipper)
										i := 0

										var collection *dal.Collection

										for scanner.Scan() {
											if err := scanner.Err(); err != nil {
												errAndMaybeQuit(c, "Failed to read line %d in %s: %v", i, importName, err)
												continue ImportFile
											} else if i == 0 {
												// read the first line, which should be the collection schema descriptor
												if err := json.Unmarshal(scanner.Bytes(), &collection); err == nil {
													if schema, ok := db.Schema[collection.Name]; ok {
														if diff := collection.Diff(schema); len(diff) > 0 {
															diffs := ``

															for _, d := range diff {
																diffs += d.String() + `\n`
															}

															if !c.Bool(`force`) {
																errAndMaybeQuit(c, "Incoming schema %q differs from definition:\n%v", collection.Name, diffs)
																continue ImportFile
															} else {
																log.Warningf("Incoming schema %q differs from definition:\n%v", collection.Name, diffs)
															}
														}

														collection = schema
														log.Noticef("Importing collection %q", collection.Name)

														if c.Bool(`dump-only`) {
															if data, err := json.Marshal(schema); err == nil {
																fmt.Println(string(data))
															} else {
																log.Warningf("Failed to re-marshal collection %v: %v", collection.Name, err)
															}
														}
													} else {
														errAndMaybeQuit(c, "Unrecognized schema %q in %v", collection.Name, importName)
														continue ImportFile
													}
												} else {
													errAndMaybeQuit(c, "Failed to read collection descriptor in %s: %v", importName, err)
												}
											} else if collection != nil {
												if model, ok := db.Models[collection.Name]; ok && model != nil {
													// all subsequent lines represent individual records
													var record *dal.Record

													if err := json.Unmarshal(scanner.Bytes(), &record); err == nil {
														if c.Bool(`dump-only`) {
															if data, err := json.Marshal(record); err == nil {
																fmt.Println(string(data))
															} else {
																log.Warningf("Failed to re-marshal record %v: %v", record.ID, err)
															}
														} else {
															if err := model.CreateOrUpdate(record.ID, record); err == nil {
																log.Debugf("%s: imported %v", collection.Name, record.ID)
															} else {
																errAndMaybeQuit(c, "Failed to import record %v line %d in %s: %v", record.ID, i, importName, err)
															}
														}
													} else {
														errAndMaybeQuit(c, "Failed to read record on line %d in %s: %v", i, importName, err)
													}
												} else {
													errAndMaybeQuit(c, "Failed to retrieve model %q", collection.Name)
												}
											} else {
												break
											}

											i += 1
										}

										gunzipper.Close()
									} else {
										log.Fatalf("failed to unzip stream: %v", err)
									}
								} else {
									errAndMaybeQuit(c, "%v", err)
								}
							}
						}
					},
				},
			},
		},
	}
}
