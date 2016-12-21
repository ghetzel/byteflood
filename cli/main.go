package main

import (
	"fmt"
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/scanner"
	// "github.com/ghetzel/byteflood/shares"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/op/go-logging"
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

	var config byteflood.Configuration

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
			config = *cf
		} else {
			log.Fatal(err)
		}

		if err := byteflood.MergeConfigDirectory(&config, c.String(`config-dir`)); err != nil {
			log.Fatal(err)
		}

		log.Debugf("Loaded configuration:\n%s\n", config.String())

		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:      `run`,
			Usage:     `Start a file transfer peer using the given configuration`,
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
				cli.StringFlag{
					Name:  `api-address`,
					Usage: `The address the local API should listen on`,
					Value: ``,
				},
				cli.IntFlag{
					Name:  `api-port`,
					Usage: `The port the local API should listen on`,
					Value: 10451,
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
				if c.IsSet(`download-limit`) {
					config.DownloadCap = c.Int(`download-limit`)
				}

				if c.IsSet(`upload-limit`) {
					config.UploadCap = c.Int(`upload-limit`)
				}

				if c.IsSet(`upnp`) {
					config.EnableUpnp = c.Bool(`upnp`)
				}

				config.PeerAddress = fmt.Sprintf("%s:%d", c.String(`address`), c.Int(`port`))
				config.ApiAddress = fmt.Sprintf("%s:%d", c.String(`api-address`), c.Int(`api-port`))

				if app, err := createApplication(config); err == nil {
					if err := app.Run(); err != nil {
						log.Fatal(err)
					}
				} else {
					log.Fatal(err)
				}
			},
		}, {
			Name:  `scan`,
			Usage: `Scans all configured source directories for changes`,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name: `quick, q`,
					Usage: `Only import files not already in the metadata database.`,
				},
			},
			Action: func(c *cli.Context) {
				if c.IsSet(`quick`) {
					config.ScanOptions.QuickScan = c.Bool(`quick`)
				}

				if app, err := createApplication(config); err == nil {
					if err := app.ScanAll(); err != nil {
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
		},{
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
				cli.StringFlag{
					Name: `db`,
					Usage: `Query the named database`,
					Value: scanner.DefaultCollectionName,
				},
			},
			Action: func(c *cli.Context) {
				if app, err := createApplication(config); err == nil {
					if rs, err := app.Scanner().QueryRecordsFromCollection(
						c.String(`db`),
						strings.Join(c.Args(), `/`),
					); err == nil {
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
				if app, err := createApplication(config); err == nil {
					if err := app.Scanner().CleanRecords(); err != nil {
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

func createApplication(config byteflood.Configuration) (*byteflood.Application, error) {
	if app, err := byteflood.CreateApplication(config); err == nil {
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
