package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ghetzel/byteflood/client"
	"github.com/ghetzel/cli"
)

func subcommandsAPI() []cli.Command {
	return []cli.Command{
		{
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
		},
	}
}
