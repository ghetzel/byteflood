package main

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/scanner"
	"github.com/ghetzel/cli"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
)

var log = logging.MustGetLogger(`byteflood.cli`)

func main() {
	app := cli.NewApp()
	app.Name = `byteflood`
	app.Usage = `Manages the automatic creation and serving of files via the BitTorrent protocol`
	app.Version = `0.0.1`
	app.EnableBashCompletion = false

	var config byteflood.Configuration
	var btPeer *peer.Peer

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
	}

	app.Before = func(c *cli.Context) error {
		log.Infof("Starting %s %s", c.App.Name, c.App.Version)
		log.Infof("Loading configuration from %s", c.String(`config`))

		if c, err := byteflood.LoadConfig(c.String(`config`)); err == nil {
			config = c
		} else {
			return err
		}

		return nil
	}

	app.After = func(c *cli.Context) error {
		if btPeer != nil {
			btPeer.Close()
		}

		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:      `seed`,
			Usage:     `Continuously seeds the torrents that are found in a given directory and all subdirectories`,
			ArgsUsage: `PATH`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address the client should listen on`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the client should listen on`,
				},
				cli.StringFlag{
					Name:  `stats-file, S`,
					Usage: `The name of a file to periodically log client stats information to`,
				},
				cli.DurationFlag{
					Name:  `import-interval, I`,
					Usage: `How often the directory should be rechecked for new torrents to seed`,
					Value: peer.DEFAULT_IMPORT_INTERVAL,
				},
			},
			Action: func(c *cli.Context) {
				if c.NArg() == 0 {
					log.Fatalf("Must specify a directory to seed.")
				}

				listenAddr := c.String(`address`)

				if port := c.Int(`port`); port != 0 {
					listenAddr = fmt.Sprintf("%s:%d", listenAddr, port)
				}

				if dirClient, err := peer.CreatePeer(c.Args().First(), listenAddr); err == nil {
					btPeer = dirClient

					if v := c.String(`stats-file`); v != `` {
						btPeer.StatsFile = v
					}

					if v := c.Duration(`import-interval`); v > 0 {
						btPeer.ImportInterval = v
					}

					signalChan := make(chan os.Signal, 1)
					signal.Notify(signalChan, os.Interrupt)

					go func(p *peer.Peer) {
						for _ = range signalChan {
							p.Close()
							break
						}

						os.Exit(0)
					}(btPeer)

					if err := btPeer.Run(); err != nil {
						log.Fatalf("Client error: %v", err)
					}
				} else {
					log.Fatalf("Failed to initialize client: %v", err)
				}
			},
		}, {
			Name:  `serve`,
			Usage: `Start serving the BitTorrent tracker and management application`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `address, a`,
					Usage: `The address the tracker should listen on`,
					Value: `127.0.0.1`,
				},
				cli.IntFlag{
					Name:  `port, p`,
					Usage: `The port the server should listen on`,
					Value: 6969,
				},
			},
			Action: func(c *cli.Context) {
				address := c.String(`address`)
				port := c.Int(`port`)

				server := byteflood.NewServer(address, port)

				if err := server.ListenAndServe(); err != nil {
					log.Fatal(err)
				}
			},
		}, {
			Name:      `scan`,
			Usage:     `Scans all configured source directories for changes and automatically manages infohash (.torrent) files`,
			ArgsUsage: `PATH [.. PATH]`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  `pattern, p`,
					Usage: `A Perl-compatible regular expression that filenames must match to be included in the scan`,
				},
				cli.IntFlag{
					Name:  `piece-length, l`,
					Usage: `The size of each piece in the generated infohashes`,
					Value: scanner.DEFAULT_BF_HASH_PIECELENGTH,
				},
				cli.StringSliceFlag{
					Name:  `announce, a`,
					Usage: `The URL for the tracker to contact for peer discovery (can be specified multiple times)`,
				},
			},
			Action: func(c *cli.Context) {
				applyFlagsToConfig(c, &config)
				paths := c.Args()

				for _, path := range paths {
					if err := scanPath(path, &config, false, config.ScanOptions); err != nil {
						log.Error(err)
						continue
					}
				}
			},
		}, {
			Name:      `hash`,
			Usage:     `Generates a BitTorrent infohash (.torrent) for the given file or directory`,
			ArgsUsage: `PATH`,
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:  `piece-length, l`,
					Usage: `The size of each piece in the infohash.`,
					Value: scanner.DEFAULT_BF_HASH_PIECELENGTH,
				},
				cli.StringSliceFlag{
					Name:  `announce, a`,
					Usage: `The URL for the tracker to contact for peer discovery (can be specified multiple times)`,
				},
			},
			Action: func(c *cli.Context) {
				applyFlagsToConfig(c, &config)

				if c.NArg() == 1 {
					if err := scanPath(c.Args().First(), &config, true, &scanner.ScannerOptions{
						ForceTorrentRehash: true,
					}); err != nil {
						log.Fatal(err)
					}
				} else {
					log.Fatalf("Must specify one path to generate a .torrent for")
				}
			},
		}, {
			Name:      `dump`,
			Usage:     `Reads and prints a .torrent file to standard out`,
			ArgsUsage: `PATH [.. PATH]`,
			Action: func(c *cli.Context) {
				if c.NArg() == 0 {
					log.Fatalf("Must specify at least one torrent file to dump")
				}

				torrents := make([]scanner.Torrent, 0)

				for _, path := range c.Args() {
					if data, err := ioutil.ReadFile(path); err == nil {
						if torrent, err := scanner.ReadTorrent(data); err == nil {
							torrents = append(torrents, torrent)
						} else {
							log.Fatalf("Failed to parse torrent %s: %v", path, err)
						}
					} else {
						log.Fatalf("Failed to read %s: %v", path, err)
					}
				}

				if data, err := json.MarshalIndent(torrents, ``, `  `); err == nil {
					fmt.Println(string(data[:]))
				} else {
					log.Fatalf("Failed to print torrents: %v", err)
				}
			},
		},
	}

	app.Run(os.Args)
}

func scanPath(path string, config *byteflood.Configuration, oneShot bool, options *scanner.ScannerOptions) error {
	s := scanner.NewScanner(path, config.ScanPattern)

	s.PieceLength = config.PieceLength
	s.AnnounceList = config.AnnounceList

	if config.DirectoryPrefix != `` {
		s.DirectoryPrefix = config.DirectoryPrefix
	} else {
		if cwd, err := os.Getwd(); err == nil {
			s.DirectoryPrefix = cwd
		} else {
			return err
		}
	}

	if oneShot {
		if _, err := s.ScanFile(path, options); err != nil {
			return fmt.Errorf("Failed to scan file %q: %v", path, err)
		}
	} else {
		if err := s.Scan(options); err != nil {
			return fmt.Errorf("Failed to scan path %q: %v", path, err)
		}
	}

	return nil
}

func applyFlagsToConfig(c *cli.Context, config *byteflood.Configuration) {
	if v := c.Int(`piece-length`); v != 0 {
		config.PieceLength = v
	}

	if v := c.StringSlice(`announce`); len(v) > 0 {
		config.AnnounceList = v
	}

	if v := c.String(`pattern`); v != `` {
		config.ScanPattern = v
	}

	if config.ScanOptions == nil {
		config.ScanOptions = scanner.DefaultScannerOptions()
	}

	log.Infof("Options: %+v", config.ScanOptions)
	log.Infof("Piece Length: %d", config.PieceLength)
	log.Infof("Announce: %s", strings.Join(config.AnnounceList, `,`))

	if config.ScanPattern != `` {
		log.Infof("Scan Pattern: '%s'", config.ScanPattern)
	}

	log.Infof("================================================================================")
}
