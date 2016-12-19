package main

import (
	"fmt"
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/scanner"
	"github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/op/go-logging"
	"io"
	"net"
	"os"
	"os/signal"
	"text/tabwriter"
	"encoding/json"
	"github.com/ghodss/yaml"
	"encoding/xml"
	"strings"
)

const DEFAULT_FORMAT = `text`

var log = logging.MustGetLogger(`main`)

func main() {
	app := cli.NewApp()
	app.Name = `byteflood`
	app.Usage = `Manages the automatic creation and serving of files via the BitTorrent protocol`
	app.Version = `0.0.1`
	app.EnableBashCompletion = false

	var config *byteflood.Configuration

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   `log-level, L`,
			Usage:  `Level of log output verbosity`,
			Value:  `debug`,
			EnvVar: `LOGLEVEL`,
		},
		cli.StringFlag{
			Name:  `config, c`,
			Usage: `The path to the configuration file`,
			Value: `~/.config/byteflood/config.yml`,
		},
		cli.StringFlag{
			Name:  `config-dir, C`,
			Usage: `The path to a directory containing additional configuration files`,
			Value: `~/.config/byteflood/conf.d/`,
		},
		cli.StringFlag{
			Name:  `public-key, k`,
			Usage: `The path to the file containing the local public key`,
		},
		cli.StringFlag{
			Name:  `private-key, K`,
			Usage: `The path to the file containing the local private key`,
		},
	}

	app.Before = func(c *cli.Context) error {
		logging.SetFormatter(logging.MustStringFormatter(`%{color}%{level:.4s}%{color:reset}[%{id:04d}] %{message}`))

		if level, err := logging.LogLevel(c.String(`log-level`)); err == nil {
			logging.SetLevel(level, ``)
		} else {
			return err
		}

		log.Infof("Starting %s %s", c.App.Name, c.App.Version)
		log.Infof("Loading configuration from %s", c.String(`config`))

		if v := c.GlobalString(`public-key`); v != `` {
			byteflood.DefaultConfig.PublicKey = v
		}

		if v := c.GlobalString(`private-key`); v != `` {
			byteflood.DefaultConfig.PrivateKey = v
		}

		if cf, err := byteflood.LoadConfigDefaults(c.String(`config`)); err == nil {
			config = cf
		} else {
			log.Fatal(err)
		}

		if err := byteflood.MergeConfigDirectory(config, c.String(`config-dir`)); err != nil {
			log.Fatal(err)
		}

		log.Debugf("Loaded configuration:\n%s\n", config.String())

		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:      `run`,
			Usage:     `Start a file transfer peer using the given configuration`,
			ArgsUsage: `PATH`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address the client should listen on`,
					Value: `0.0.0.0`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the client should listen on`,
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
				cli.StringSliceFlag{
					Name: `peer, P`,
					Usage: `Specify the address of a peer to connect to`,
				},
			},
			Action: func(c *cli.Context) {
				if localPeer, err := makeLocalPeer(config, c); err == nil {
					localPeer.EnableUpnp = c.Bool(`upnp`)

					if v := c.Int(`download-limit`); v > 0 {
						localPeer.DownloadBytesPerSecond = v
					}

					if v := c.Int(`upload-limit`); v > 0 {
						localPeer.UploadBytesPerSecond = v
					}

					localPeer.PeerAddresses = c.StringSlice(`peer`)

					if err := localPeer.Run(); err != nil {
						log.Fatal(err)
					}
				} else {
					log.Fatal(err)
				}
			},
		}, {
			Name:  `send`,
			Usage: `[TEST] Connect to a remote peer`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address the client should listen on`,
					Value: `0.0.0.0`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the client should listen on`,
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
			ArgsUsage: `ADDRESS:PORT`,
			Action: func(c *cli.Context) {
				address := c.Args().First()

				if host, portStr, err := net.SplitHostPort(address); err == nil {
					if port, err := stringutil.ConvertToInteger(portStr); err == nil {
						if localPeer, err := makeLocalPeer(config, c); err == nil {
							if v := c.Int(`download-limit`); v > 0 {
								localPeer.DownloadBytesPerSecond = v
							}

							if v := c.Int(`upload-limit`); v > 0 {
								localPeer.UploadBytesPerSecond = v
							}

							if remotePeer, err := localPeer.ConnectTo(host, int(port)); err == nil {
								log.Infof("Connected to peer: %s", remotePeer.String())

								if c.NArg() > 1 {
									if err := remotePeer.TransferFile(c.Args().Get(1)); err != nil {
										log.Fatal(err)
									}
								} else {
									if n, err := io.Copy(remotePeer, os.Stdin); err != nil {
										log.Fatalf("Error during write (wrote %d bytes): %v", n, err)
									}
								}
							} else {
								log.Fatal(err)
							}
						} else {
							log.Fatal(err)
						}
					} else {
						log.Fatal(err)
					}
				} else {
					log.Fatal(err)
				}
			},
		}, {
			Name:  `request`,
			Usage: `[TEST] Connect to a remote peer`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address the client should listen on`,
					Value: `0.0.0.0`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the client should listen on`,
				},
			},
			Action: func(c *cli.Context) {
				if c.NArg() == 3 {
					address := c.Args().First()
					method := c.Args().Get(1)
					path := c.Args().Get(2)

					if host, portStr, err := net.SplitHostPort(address); err == nil {
						if port, err := stringutil.ConvertToInteger(portStr); err == nil {
							if localPeer, err := makeLocalPeer(config, c); err == nil {
								if remotePeer, err := localPeer.ConnectTo(host, int(port)); err == nil {
									log.Infof("Connected to peer: %s", remotePeer.String())

									if response, err := remotePeer.ServiceRequest(method, path, nil, nil); err == nil {
										log.Infof("Response: HTTP %s", response.Status)
										log.Infof("    Length: %d", response.ContentLength)

										for k, v := range response.Header {
											log.Infof("    Header: %s=%s", k, strings.Join(v, ` `))
										}

										io.Copy(os.Stdout, response.Body)

										if response.StatusCode < 400 {
											os.Exit(0)
										} else {
											os.Exit(1)
										}
									} else {
										log.Fatal(err)
									}
								} else {
									log.Fatal(err)
								}
							} else {
								log.Fatal(err)
							}
						} else {
							log.Fatal(err)
						}
					} else {
						log.Fatal(err)
					}
				} else {
					log.Fatalf("Requires 3 arguments: PEERADDR METHOD PATH")
				}
			},
		}, {
			Name:  `scan`,
			Usage: `Scans all configured source directories for changes`,
			Action: func(c *cli.Context) {
				if s, err := makeScanner(config); err == nil {
					if err := s.Scan(); err != nil {
						log.Fatalf("Failed to scan: %v", err)
					}
				} else {
					log.Fatal(err)
				}
			},
		}, {
			Name:      `genkeypair`,
			Usage:     `Generates a new public/private key pair and saves them to files`,
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
			Name: `testshares`,
			Usage: `[TEST] Test working with share views`,
			Action: func(c *cli.Context) {
				if c.NArg() == 0 {
					log.Fatalf("Must specify a base filter to test a share")
				}

				if s, err := makeScanner(config); err == nil {
					share := shares.NewShare(s, c.Args().First())

					log.Infof("Share Length: %d", share.Length())

					if c.NArg() > 1 {
						if records, err := share.Find(c.Args().Get(1)); err == nil {
							for i, record := range records {
								log.Infof("%03d: %+v", i, record)
							}
						}else{
							log.Fatal(err)
						}
					}
				} else {
					log.Fatal(err)
				}
			},
		}, {
			Name: `query`,
			Usage: `Query the metadata database.`,
			ArgsUsage: `FILTER`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `format, f`,
					Usage: `The output format to use when printing results`,
					Value: DEFAULT_FORMAT,
				},
				cli.StringSliceFlag{
					Name: `field, F`,
					Usage: `Additional fields to include in output tables in addition to ID and path`,
				},
			},
			Action: func(c *cli.Context) {
				if s, err := makeScanner(config); err == nil {
					if rs, err := s.QueryRecords(strings.Join(c.Args(), `/`)); err == nil {
						printWithFormat(c.String(`format`), rs, func() {
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
			Name: `cleanup`,
			Usage: `Cleanup the metadata database.`,
			Action: func(c *cli.Context) {
				if s, err := makeScanner(config); err == nil {
					if err := s.CleanRecords(); err != nil {
						log.Fatal(err)
					}
				}else{
					log.Fatal(err)
				}
			},
		},
	}

	app.Run(os.Args)
}

func makeLocalPeer(config *byteflood.Configuration, c *cli.Context) (*peer.LocalPeer, error) {
	if publicKey, privateKey, err := encryption.LoadKeyfiles(
		config.PublicKey,
		config.PrivateKey,
	); err == nil {
		if localPeer, err := peer.CreatePeer(
			publicKey,
			privateKey,
		); err == nil {
			localPeer.Address = c.String(`address`)
			localPeer.Port = c.Int(`port`)

			signalChan := make(chan os.Signal, 1)
			signal.Notify(signalChan, os.Interrupt)

			go func(p *peer.LocalPeer) {
				for _ = range signalChan {
					<-p.Stop()
					break
				}

				os.Exit(0)
			}(localPeer)

			return localPeer, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func makeScanner(config *byteflood.Configuration) (*scanner.Scanner, error) {
	s := scanner.NewScanner()

	if err := s.Initialize(); err == nil {
		for _, directory := range config.Directories {
			if err := s.AddDirectory(&directory); err != nil {
				return nil, err
			}
		}

		return s, nil
	}else{
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
