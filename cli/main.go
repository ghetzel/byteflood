package main

import (
	"encoding/json"
	"fmt"
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/scanner"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
	"strings"
)

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
			Value:  `info`,
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

	app.Commands = []cli.Command{
		{
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
				cli.StringSliceFlag{
					Name:  `tag, t`,
					Usage: `Tags to pass to the scanner process`,
				},
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
					if err := scanPath(path, &config, c.StringSlice(`tag`)...); err != nil {
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
					if err := scanPath(c.Args().First(), &config, `rehash`, `oneshot`); err != nil {
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

func scanPath(path string, config *byteflood.Configuration, tags ...string) error {
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

	t := append(config.ScanTags, tags...)

	if sliceutil.ContainsString(t, `oneshot`) {
		if _, err := s.ScanFile(path, t...); err != nil {
			return fmt.Errorf("Failed to scan file %q: %v", path, err)
		}
	} else {
		if err := s.Scan(t...); err != nil {
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

	if v := c.StringSlice(`tag`); len(v) > 0 {
		config.ScanTags = v
	}

	log.Infof("Tags: %s", strings.Join(config.ScanTags, `,`))
	log.Infof("Piece Length: %d", config.PieceLength)
	log.Infof("Announce: %s", strings.Join(config.AnnounceList, `,`))

	if config.ScanPattern != `` {
		log.Infof("Scan Pattern: '%s'", config.ScanPattern)
	}

	log.Infof("================================================================================")
}
