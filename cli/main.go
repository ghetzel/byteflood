package main

import (
	"fmt"
	"io"
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/client"
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
	app.Usage = byteflood.Description
	app.Version = byteflood.Version
	app.EnableBashCompletion = false

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
				if app, err := createApplication(c); err == nil {
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
				if app, err := createApplication(c); err == nil {
					if err := app.Scan(c.Args()...); err != nil {
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
					Value: db.DefaultMetadataCollectionName,
				},
			},
			Action: func(c *cli.Context) {
				if app, err := createApplication(c); err == nil {
					if f, err := app.Database.ParseFilter(strings.Join(c.Args(), `/`)); err == nil {
						if rs, err := app.Database.Query(c.String(`db`), f); err == nil {
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
					}else{
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
				if app, err := createApplication(c); err == nil {
					if err := app.Database.CleanRecords(); err != nil {
						log.Fatal(err)
					}
				}else{
					log.Fatal(err)
				}
			},
		}, {
			Name: `call`,
			Usage: `Perform an HTTP call against the Byteflood API`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `method, m`,
					Usage: `The HTTP method to use for the call`,
					Value: `get`,
				},
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address of the Byteflood API to contact`,
					Value: client.DefaultAddress,
				},
				cli.DurationFlag{
					Name: `timeout, t`,
					Usage: `HTTP request timeout`,
					Value: client.DefaultRequestTimeout,
				},
				cli.StringSliceFlag{
					Name:  `param, p`,
					Usage: `An HTTP query string parameter to include in the call, formatted as "name=value"`,
				},
				cli.StringSliceFlag{
					Name:  `header, H`,
					Usage: `An HTTP header to include in the call, formatted as "name=value"`,
				},
				cli.StringFlag{
					Name:  `format, f`,
					Usage: `The output format to use when printing results`,
					Value: `json`,
				},
			},
			Action: func(c *cli.Context) {
				if c.NArg() == 0 {
					log.Fatalf("Must specify an API endpoint path to call")
					return
				}

				bf := client.NewClient()
				params := make(map[string]string)
				headers := make(map[string]string)

				bf.Address = c.String(`address`)
				bf.Timeout = c.Duration(`timeout`)

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

				if response, err := bf.Request(
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

					if data, err := client.ParseResponse(response); err == nil && data != nil {
						printWithFormat(c.String(`format`), data, func(){
							fmt.Printf("%v\n", data)
						})
					}

					if response.StatusCode < 400 {
						os.Exit(0)
					}else{
						os.Exit(1)
					}
				}else{
					log.Fatal(err)
				}
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
		}else{
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
