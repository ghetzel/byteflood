package main

import (
	"fmt"
	"github.com/ghetzel/byteflood"
	"github.com/ghetzel/byteflood/encryption"
	"github.com/ghetzel/byteflood/peer"
	"github.com/ghetzel/byteflood/scanner"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/op/go-logging"
	"io"
	"net"
	"os"
	"os/signal"
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
			Value: `~/.config/byteflood/keys/peer.pub`,
		},
		cli.StringFlag{
			Name:  `private-key, K`,
			Usage: `The path to the file containing the local private key`,
			Value: `~/.config/byteflood/keys/peer.key`,
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

		if cf, err := byteflood.LoadConfig(c.String(`config`)); err == nil {
			config = cf
		} else {
			log.Fatal(err)
		}

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
			},
			Action: func(c *cli.Context) {
				if localPeer, err := makeLocalPeer(&config, c); err == nil {
					localPeer.EnableUpnp = c.Bool(`upnp`)

					if v := c.Int(`download-limit`); v > 0 {
						localPeer.DownloadBytesPerSecond = v
					}

					if v := c.Int(`upload-limit`); v > 0 {
						localPeer.UploadBytesPerSecond = v
					}

					if err := localPeer.Listen(); err != nil {
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
						if localPeer, err := makeLocalPeer(&config, c); err == nil {
							if v := c.Int(`download-limit`); v > 0 {
								localPeer.DownloadBytesPerSecond = v
							}

							if v := c.Int(`upload-limit`); v > 0 {
								localPeer.UploadBytesPerSecond = v
							}

							if remotePeer, err := localPeer.ConnectTo(host, int(port)); err == nil {
								log.Infof("Connected to peer: %s", remotePeer.String())

								if c.NArg() > 1 {
									if file, err := os.Open(c.Args().Get(1)); err == nil {
										if stat, err := file.Stat(); err == nil {
											if err := remotePeer.BeginChecked(int(stat.Size())); err != nil {
												log.Fatalf("Error starting checked transfer: %v", err)
											}

											if n, err := io.Copy(remotePeer, file); err != nil {
												log.Fatalf("Error during write (wrote %d bytes): %v", n, err)
											}

											if err := remotePeer.FinishChecked(); err != nil {
												log.Warningf("Checked transfer finalize failed: %v", err)
											}

											if err := remotePeer.Disconnect(); err != nil {
												log.Errorf("Disconnect failed: %v", err)
											}
										} else {
											log.Fatal(err)
										}
									} else {
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
			Name:  `scan`,
			Usage: `Scans all configured source directories for changes`,
			Action: func(c *cli.Context) {
				s := scanner.NewScanner()

				if err := s.Initialize(); err == nil {
					for _, directory := range config.Directories {
						s.AddDirectory(directory.Path, directory.Options)
					}

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
		},
	}

	app.Run(os.Args)
}

func makeLocalPeer(config *byteflood.Configuration, c *cli.Context) (*peer.LocalPeer, error) {
	if publicKey, privateKey, err := encryption.LoadKeyfiles(
		c.GlobalString(`public-key`),
		c.GlobalString(`private-key`),
	); err == nil {
		if localPeer, err := peer.CreatePeer(
			config.ID,
			publicKey,
			privateKey,
		); err == nil {
			localPeer.Address = c.String(`address`)
			localPeer.Port = c.Int(`port`)

			signalChan := make(chan os.Signal, 1)
			signal.Notify(signalChan, os.Interrupt)

			go func(p *peer.LocalPeer) {
				for _ = range signalChan {
					<-p.Close()
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
