package main

import (
	"fmt"
	"os"
	"path"
	"strings"
	"text/tabwriter"

	"github.com/ghetzel/byteflood/client"
	"github.com/ghetzel/byteflood/db"
	"github.com/ghetzel/byteflood/util"
	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
)

func subcommandsPeers() []cli.Command {
	return []cli.Command{
		{
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
		},
	}
}
