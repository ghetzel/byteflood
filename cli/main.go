package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/client"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/byteflood/stats"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghodss/yaml"
	"github.com/op/go-logging"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"strings"
	"text/tabwriter"
)

const DEFAULT_FORMAT = `text`

var log = logging.MustGetLogger(`main`)
var DefaultLogLevel = map[string]logging.Level{
	`run`: logging.DEBUG,
	``:    logging.NOTICE,
}

func main() {
	app := cli.NewApp()
	app.Name = `byteflood`
	app.Usage = byteflood.Description
	app.Version = byteflood.Version
	app.EnableBashCompletion = true

	var application *byteflood.Application
	var database *db.Database
	api := client.NewClient()

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   `log-level, L`,
			Usage:  `Level of log output verbosity`,
			EnvVar: `LOGLEVEL`,
		},
		cli.StringFlag{
			Name:  `peer-address, a`,
			Usage: `This is the public-facing address that should be exposed to peers.`,
			Value: peer.DEFAULT_PEER_SERVER_ADDRESS,
		},
		cli.StringFlag{
			Name:  `api-address, A`,
			Usage: `This is the private internal administrative API address.`,
			Value: byteflood.DefaultApiAddress,
		},
		cli.BoolFlag{
			Name:  `log-queries, Q`,
			Usage: `Whether to include queries in the logging output`,
		},
		cli.StringFlag{
			Name:  `config, c`,
			Usage: `The path to the configuration file`,
			Value: `~/.config/byteflood/config.yml`,
		},
		cli.StringFlag{
			Name:  `public-key, k`,
			Usage: `The path to the file containing the local public key`,
		},
		cli.StringFlag{
			Name:  `private-key, K`,
			Usage: `The path to the file containing the local private key`,
		},
		cli.StringFlag{
			Name:  `format, f`,
			Usage: `The output format to print records as (one of: json, yaml, xml, text)`,
			Value: DEFAULT_FORMAT,
		},
		cli.DurationFlag{
			Name:  `timeout, t`,
			Usage: `Byteflood client request timeout`,
			Value: client.DefaultRequestTimeout,
		},
		cli.BoolTFlag{
			Name:  `collect-stats`,
			Usage: `Whether to collect and store statistics within the application data directory.`,
		},
	}

	app.Before = func(c *cli.Context) error {
		logging.SetFormatter(logging.MustStringFormatter(`%{color}%{level:.4s}%{color:reset}[%{id:04d} %{shortfile:30s}] %{message}`))

		if logLevel := c.String(`log-level`); logLevel == `` {
			if lvl, ok := DefaultLogLevel[c.Args().First()]; ok {
				logging.SetLevel(lvl, ``)
			} else if lvl, ok := DefaultLogLevel[``]; ok {
				logging.SetLevel(lvl, ``)
			} else {
				logging.SetLevel(logging.DEBUG, ``)
			}
		} else {
			if level, err := logging.LogLevel(logLevel); err == nil {
				logging.SetLevel(level, ``)
			} else {
				return err
			}
		}

		if c.Bool(`log-queries`) {
			logging.SetLevel(logging.DEBUG, `pivot/querylog`)
		} else {
			logging.SetLevel(logging.CRITICAL, `pivot/querylog`)
		}

		logging.SetLevel(logging.ERROR, `diecast`)

		api.Timeout = c.Duration(`timeout`)
		api.Address = c.String(`api-address`)
		stats.LocalStatsEnabled = c.Bool(`collect-stats`)

		log.Debugf("API address is %s", api.Address)

		log.Infof("Starting %s %s", c.App.Name, c.App.Version)

		if !sliceutil.ContainsString([]string{
			`genkeypair`,
		}, c.Args().First()) {
			if a, err := createApplication(c); err == nil {
				application = a
				database = a.Database
				application.LocalPeer.Address = c.String(`peer-address`)
			} else {
				return err
			}
		}

		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:  `run`,
			Usage: `Start a file transfer peer using the given configuration`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `ui-dir`,
					Usage: `The path to the UI directory.`,
					Value: byteflood.DefaultUiDirectory,
				},
				cli.BoolFlag{
					Name:  `upnp, u`,
					Usage: `Automatically forward this port using UPnP`,
				},
				cli.IntFlag{
					Name:  `upload-limit, U`,
					Usage: `Limit uploads to this many bytes per second`,
				},
				cli.IntFlag{
					Name:  `download-limit, D`,
					Usage: `Limit downloads to this many bytes per second`,
				},
			},
			Action: func(c *cli.Context) {
				application.API.UiDirectory = c.String(`ui-dir`)
				application.LocalPeer.EnableUpnp = c.Bool(`upnp`)

				if err := application.Run(); err != nil {
					log.Fatal(err)
				}
			},
		}, {
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
			Name:  `id`,
			Usage: `Print your local peer ID that is shared with other peers.`,
			Action: func(c *cli.Context) {
				fmt.Printf("%v\n", application.LocalPeer.GetID())
			},
		}, {
			Name:      `genkeypair`,
			Usage:     `Generates a new public/private key pair and saves them to files in the current directory.`,
			ArgsUsage: `BASENAME`,
			Action: func(c *cli.Context) {
				if c.NArg() > 0 {
					if err := encryption.GenerateKeypair(
						fmt.Sprintf("%s.pub", c.Args().First()),
						fmt.Sprintf("%s.key", c.Args().First()),
					); err != nil {
						log.Fatalf("Failed to generate keys: %v", err)
					}
				} else {
					log.Fatalf("Must specify a base filename")
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
			Name:  `cleanup`,
			Usage: `Cleanup the metadata database.`,
			Action: func(c *cli.Context) {
				if err := application.Database.Cleanup(); err != nil {
					log.Fatal(err)
				}
			},
		}, {
			Name:  `api`,
			Usage: `Perform an HTTP call against the Byteflood API`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `method, m`,
					Usage: `The HTTP method to use for the call`,
					Value: `get`,
				},
				cli.StringSliceFlag{
					Name:  `param, p`,
					Usage: `An HTTP query string parameter to include in the call, formatted as "name=value"`,
				},
				cli.StringSliceFlag{
					Name:  `header, H`,
					Usage: `An HTTP header to include in the call, formatted as "name=value"`,
				},
			},
			Action: func(c *cli.Context) {
				if c.NArg() == 0 {
					log.Fatalf("Must specify an API endpoint path to call")
					return
				}

				params := make(map[string]string)
				headers := make(map[string]string)

				for _, pair := range c.StringSlice(`header`) {
					if kv := strings.SplitN(pair, `=`, 2); len(kv) == 2 {
						headers[kv[0]] = kv[1]
					}
				}

				for _, pair := range c.StringSlice(`param`) {
					if kv := strings.SplitN(pair, `=`, 2); len(kv) == 2 {
						params[kv[0]] = kv[1]
					}
				}

				path := c.Args().First()
				path = strings.TrimPrefix(path, `/`)
				path = fmt.Sprintf("/api/%s", path)

				var input io.Reader

				if stat, err := os.Stdin.Stat(); err == nil {
					if (stat.Mode() & os.ModeCharDevice) == 0 {
						input = os.Stdin
					}
				}

				if response, err := api.Request(
					c.String(`method`),
					path,
					params,
					headers,
					input,
				); err == nil {
					log.Debugf("Response: %s", response.Status)

					for k, v := range response.Header {
						log.Debugf("  %s = %s", k, strings.Join(v, `, `))
					}

					data, _ := client.ParseResponse(response)

					if data != nil {
						printWithFormat(c.GlobalString(`format`), data, func() {
							fmt.Printf("%v\n", data)
						})
					}

					if response.StatusCode < 400 {
						os.Exit(0)
					} else {
						os.Exit(1)
					}
				} else {
					log.Fatal(err)
				}
			},
		}, {
			Name:  `peers`,
			Usage: `Manage peer authorizations and access.`,
			Subcommands: []cli.Command{
				{
					Name:      `show`,
					ArgsUsage: `[NAME ..]`,
					Usage:     `List authorized peers`,
					Action: func(c *cli.Context) {
						if authorized, err := api.GetAuthorizedPeers(); err == nil {
							printWithFormat(c.GlobalString(`format`), authorized, func() {
								tw := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)

								fmt.Fprintf(tw, "ID\tNAME\tSTATUS\tADDRESSES\n")

								peers := c.Args()
								printed := 0

								for _, peer := range authorized {
									if len(peers) > 0 {
										if !sliceutil.ContainsString(peers, peer.PeerName) {
											continue
										}
									}

									status := `inactive`

									if _, err := api.GetSession(peer.PeerName); err == nil {
										status = `connected`
									} else {
										log.Debugf("Request failed: %v", err)
									}

									fmt.Fprintf(
										tw,
										"%s\t%s\t%s\t%s\n",
										peer.ID,
										peer.PeerName,
										status,
										peer.Addresses,
									)

									printed += 1
								}

								if len(peers) > 0 && printed == 0 {
									log.Fatalf("None of the listed peers were found.")
								}

								tw.Flush()
							})
						} else {
							log.Fatal(err)
						}
					},
				}, {
					Name:      `authorize`,
					Usage:     `Authorize a peer for communication with us.`,
					ArgsUsage: `PEERID NAME`,
					Flags: []cli.Flag{
						cli.StringSliceFlag{
							Name:  `group, g`,
							Usage: `A named group this peer should belong to.`,
						},
						cli.StringSliceFlag{
							Name:  `address, a`,
							Usage: `Zero or more addresses to automatically connect to.`,
						},
					},
					Action: func(c *cli.Context) {
						peerID := c.Args().Get(0)
						name := c.Args().Get(1)

						if peerID == `` {
							log.Fatalf("Must specify a PEERID to authorize.")
						}

						if name == `` {
							log.Fatalf("Must specify a NAME for the peer.")
						}

						if _, err := api.GetAuthorizedPeer(peerID); client.IsNotFound(err) {
							if err := api.AuthorizePeer(
								peerID,
								name,
								c.StringSlice(`group`),
								c.StringSlice(`address`),
							); err == nil {
								log.Noticef("Peer ID %q successfully authorized with name %q", peerID, name)
							} else {
								log.Fatalf("Failed to authorize peer ID %q: %v", peerID, err)
							}
						} else {
							log.Noticef("Peer ID %q is already authorized.", peerID)
						}
					},
				}, {
					Name:      `revoke`,
					ArgsUsage: `PEERID`,
					Usage:     `Deauthorize a peer`,
					Action: func(c *cli.Context) {
						peerID := c.Args().Get(0)

						if peerID == `` {
							log.Fatalf("Must specify a PEERID to revoke.")
						}

						if err := api.RevokePeer(peerID); err == nil {
							log.Noticef("Peer ID %q access has been revoked.", peerID)
						} else if client.IsNotFound(err) {
							log.Warningf("Peer ID %q does not exist.", peerID)
						} else {
							log.Fatalf("Failed to revoke peer ID %q: v", peerID, err)
						}
					},
				}, {
					Name:      `connect`,
					ArgsUsage: `PEERID_OR_NAME [ADDRESS]`,
					Usage:     `Connect to a peer.`,
					Action: func(c *cli.Context) {
						// via HTTP + client
					},
				}, {
					Name:  `disconnect`,
					Usage: `Disconnect a connected peer.`,
					Action: func(c *cli.Context) {
						// via HTTP + client
					},
				}, {
					Name:      `ls`,
					ArgsUsage: `PEER [PATH]`,
					Usage:     `Browse a remote peer's shares.`,
					Flags: []cli.Flag{
						cli.BoolTFlag{
							Name:  `human, H`,
							Usage: `Print data sizes in human-readable units.`,
						},
					},
					Action: func(c *cli.Context) {
						peerOrSession := c.Args().Get(0)
						sharePath := strings.TrimPrefix(c.Args().Get(1), `/`)

						if peer, err := api.GetSession(peerOrSession); err == nil {
							if authPeer, err := api.GetAuthorizedPeer(peer.ID); err == nil {
								authPeerGroups := strings.Join(util.SplitMulti.Split(authPeer.Groups, -1), `,`)

								if sharePath == `` {
									if shares, err := api.GetShares(peerOrSession, true); err == nil {
										printWithFormat(c.GlobalString(`format`), shares, func() {
											tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)

											for _, share := range shares {
												shareSize := ``

												if share.Stats != nil {
													if share.Stats.TotalBytes > 0 {
														if c.Bool(`human`) {
															if v, err := stringutil.ToByteString(
																share.Stats.TotalBytes,
																"%.0f",
															); err == nil {
																shareSize = v
															}
														} else {
															shareSize = fmt.Sprintf("%d", share.Stats.TotalBytes)
														}
													}
												}

												fmt.Fprintf(
													tw,
													"%s\t%s\t%s\t%s\t%s\t\n",
													`sr-xr-xr-x`,
													authPeer.PeerName,
													authPeerGroups,
													shareSize,
													share.ID,
												)
											}

											tw.Flush()
										})
									} else {
										log.Fatal(err)
									}
								} else {
									parts := strings.Split(sharePath, `/`)
									shareID := parts[0]
									parent := ``

									if len(parts) > 1 {
										if rs, err := api.QueryShare(shareID, map[string]interface{}{
											`name`: fmt.Sprintf("/%s", strings.Join(parts[1:], `/`)),
										}, 1, 0, []string{
											`-directory`,
											`name`,
										}, peerOrSession); err == nil {
											if len(rs.Records) == 1 {
												parent = fmt.Sprintf("%v", rs.Records[0].ID)
											} else {
												log.Fatalf("Could not find parent ID for path %q", sharePath)
											}
										} else {
											log.Fatalf("Could not find parent ID for path %q: %v", sharePath, err)
										}
									}

									if rs, err := api.BrowseShare(shareID, parent, peerOrSession); err == nil {
										printWithFormat(c.GlobalString(`format`), rs, func() {
											tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)

											for _, record := range rs.Records {
												var entry db.Entry
												var fileSize string

												if err := record.Populate(&entry, nil); err == nil {
													if c.Bool(`human`) {
														fileSize = entry.GetHumanSize("%.0f")

														fileSize = strings.TrimSuffix(fileSize, `B`)

														if stringutil.IsInteger(fileSize) {
															fileSize = fileSize + `B`
														}
													} else {
														if v, err := stringutil.ToString(entry.Size); err == nil {
															fileSize = v
														}
													}

													if fileSize == `` {
														fileSize = `-`
													}

													fmt.Fprintf(
														tw,
														"%v\t%s\t%s\t%s\t%s\t%s\n",
														entry.Get(`file.permissions.string`, `??????????`),
														authPeer.PeerName,
														authPeerGroups,
														fileSize,
														path.Base(entry.RelativePath),
														entry.ID,
													)
												} else {
													fmt.Fprintf(
														tw,
														"%v\t%s\t%s\t-\t%s\t\n",
														`??????????`,
														authPeer.PeerName,
														authPeerGroups,
														fmt.Sprintf("err:%v", err),
													)
												}
											}

											tw.Flush()
										})
									} else {
										log.Fatal(err)
									}
								}
							} else {
								log.Fatalf("** CRITICAL**: Could not find authorization for peer %v", peer.ID)
							}
						} else {
							log.Fatal(err)
						}
					},
				}, {
					Name:  `get`,
					Usage: `Download data from a remote peer.`,
					Action: func(c *cli.Context) {
						// via HTTP + client
					},
				},
			},
		}, {
			Name:  `shares`,
			Usage: `Manage shared files`,
			Subcommands: []cli.Command{
				{
					Name:      `show`,
					ArgsUsage: `[NAME ..]`,
					Usage:     `List shares.`,
					Action: func(c *cli.Context) {
						var shares []shares.Share

						if err := db.Shares.All(&shares); err == nil {
							printWithFormat(c.GlobalString(`format`), shares, func() {
								tw := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)

								fmt.Fprintf(tw, "ID\tTITLE\tFILTER\tICON\n")

								shareFilter := c.Args()
								printed := 0

								for _, share := range shares {
									if len(shareFilter) > 0 {
										if !sliceutil.ContainsString(shareFilter, fmt.Sprintf("%v", share.ID)) {
											continue
										}
									}

									fmt.Fprintf(
										tw,
										"%s\t%s\t%s\t%s\n",
										share.ID,
										share.Description,
										share.BaseFilter,
										share.IconName,
									)

									printed += 1
								}

								if len(shareFilter) > 0 && printed == 0 {
									log.Fatalf("None of the listed shares were found.")
								}

								tw.Flush()
							})
						} else {
							log.Fatal(err)
						}
					},
				}, {
					Name:      `create`,
					Usage:     `Create a share to expose files to peers.`,
					ArgsUsage: `ID`,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  `base-filter, F`,
							Usage: `A custom database query to use for selecting which files are exposed in this share.`,
						},
						cli.StringFlag{
							Name:  `title, t`,
							Usage: `A human-readable title for the share.`,
						},
						cli.StringFlag{
							Name:  `icon, i`,
							Usage: `A fontawesome.io icon name (excluding the "fa-" prefix) to represent the share`,
							Value: `folder`,
						},
					},
					Action: func(c *cli.Context) {
						id := c.Args().First()
						var longDesc string

						if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
							if data, err := ioutil.ReadAll(os.Stdin); err == nil {
								longDesc = string(data[:])
							} else {
								log.Fatal(err)
							}
						}

						if err := api.CreateShare(id, &shares.Share{
							BaseFilter:      c.String(`base-filter`),
							Description:     c.String(`title`),
							IconName:        c.String(`icon`),
							LongDescription: longDesc,
						}); err == nil {
							log.Noticef("Share %s created successfully.", id)
						} else {
							log.Fatalf("Failed to create share %s: %v", id, err)
						}
					},
				}, {
					Name:      `remove`,
					ArgsUsage: `ID`,
					Usage:     `Remove a share.`,
					Action: func(c *cli.Context) {
						id := c.Args().Get(0)

						if id == `` {
							log.Fatalf("Must specify a share ID to remove.")
						}

						if err := api.RemoveShare(id); err == nil {
							log.Noticef("Share ID %q access has been revoked.", id)
						} else if client.IsNotFound(err) {
							log.Warningf("Share ID %q does not exist.", id)
						} else {
							log.Fatalf("Failed to remove share ID %q: v", id, err)
						}
					},
				},
			},
		}, {
			Name:  `subscriptions`,
			Usage: `Manage subscriptions to other peer's content.`,
			Subcommands: []cli.Command{
				{
					Name:      `show`,
					ArgsUsage: `[NAME ..]`,
					Usage:     `List subscriptions`,
					Action: func(c *cli.Context) {
					},
				}, {
					Name:      `create`,
					ArgsUsage: `SHARE TARGET SOURCES`,
					Usage:     `Create a new subscription.`,
					Action: func(c *cli.Context) {
					},
				}, {
					Name:      `delete`,
					ArgsUsage: `ID`,
					Usage:     `Remove a subscription.`,
					Action: func(c *cli.Context) {
					},
				}, {
					Name:      `sync`,
					ArgsUsage: `[NAME ..]`,
					Usage:     `Sync data from subscriptions.`,
					Action: func(c *cli.Context) {
					},
				},
			},
		},
	}

	app.Run(os.Args)
}

func createApplication(c *cli.Context) (*byteflood.Application, error) {
	log.Infof("Loading configuration from %s", c.GlobalString(`config`))

	if app, err := byteflood.NewApplicationFromConfig(c.GlobalString(`config`)); err == nil {
		if c.GlobalIsSet(`public-key`) {
			app.PublicKeyPath = c.GlobalString(`public-key`)
		}

		if c.GlobalIsSet(`private-key`) {
			app.PublicKeyPath = c.GlobalString(`private-key`)
		}

		if err := app.Initialize(); err == nil {
			signalChan := make(chan os.Signal, 1)
			signal.Notify(signalChan, os.Interrupt)

			go func(a *byteflood.Application) {
				for _ = range signalChan {
					a.Stop()
					break
				}

				os.Exit(0)
			}(app)

			return app, nil
		} else {
			return nil, err
		}

	} else {
		return nil, err
	}
}

func printWithFormat(format string, data interface{}, fallbackFunc ...func()) {
	var output []byte
	var err error

	switch format {
	case `json`:
		output, err = json.MarshalIndent(data, ``, `  `)
	case `yaml`:
		output, err = yaml.Marshal(data)
	case `xml`:
		output, err = xml.MarshalIndent(data, ``, `  `)
	default:
		if len(fallbackFunc) > 0 {
			f := fallbackFunc[0]
			f()
			return
		} else if data != nil {
			if v, err := stringutil.ToString(data); err == nil {
				output = []byte(v[:])
			} else {
				log.Fatal(err)
			}
		}
	}

	if err == nil {
		fmt.Println(string(output[:]))
	} else {
		log.Fatal(err)
	}
}
