# Carwings

[![GoDoc](https://godoc.org/github.com/joeshaw/carwings?status.svg)](http://godoc.org/github.com/joeshaw/carwings)

`carwings` is a Go package and command-line tool providing access to
the Nissan Leaf Carwings API.

Through the Carwings API you can ask your vehicle for the latest data,
see current battery and charging statuses, see the current climate
control state, start or stop climate control remotely, and remotely
start charging.

The `carwings` library current implements a subset of these options.

[x] Establish an authenticated connection to Carwings
[x] Ask Carwings to update vehicle data, and monitor the update
[x] Get the current battery status
[ ] Get the current climate control status
[ ] Remotely start and stop climate control
[ ] Remotely start charging

## Command-line tool

The `carwings` tool can be installed with:

    go get github.com/joeshaw/carwings/cmd/carwings

Run `carwings` by itself to see full usage information.

To update vehicle information:

    carwings -email <email> -password <password> update

To get latest battery status:

    carwings -email <email> -password <password> battery

This will print something like:

    Logging into Carwings...
    Getting latest retrieved battery status...
    Battery status as of 2017-08-06 15:43:00 -0400 EDT:
      Capacity: 240 / 240 (92%)
      Crusing range: 114 mi (107 mi with AC)
      Plug-in state: not connected
      Charging status: not charging
      Time to full:
        Level 1 charge: 8h30m0s
        Level 2 charge: 3h0m0s
        Level 2 at 6 kW: 2h30m0s

### TODO

[ ] Don't require password on the CLI
[ ] Save authentication token somewhere, so we don't re-login on every run.

## Carwings protocol

Josh Perry's [protocol reference](https://github.com/joshperry/carwings/blob/master/protocol.markdown)
was incredibly helpful for the development of this library.

Josh also has an implementation in Javascript:
https://github.com/joshperry/carwings

Jason Horne has a Python implementation:
https://github.com/jdhorne/pycarwings2

Scott Helme created a Javascript Alexa skill:
https://github.com/ScottHelme/AlexaNissanLeaf

## Contributing

Issues and pull requests are welcome.  When filing a PR, please make
sure the code has been run through `gofmt`.

## License

Copyright 2017 Joe Shaw

`carwings` is licensed under the MIT License.  See the LICENSE file
for details.
