# Carwings

[![GoDoc](https://godoc.org/github.com/joeshaw/carwings?status.svg)](http://godoc.org/github.com/joeshaw/carwings)

`carwings` is a Go package and command-line tool providing access to
the Nissan Leaf Carwings API.

Through the Carwings API you can ask your vehicle for the latest data,
see current battery and charging statuses, see the current climate
control state, start or stop climate control remotely, and remotely
start charging.

## Command-line tool

The `carwings` tool can be installed with:

    go install github.com/joeshaw/carwings/cmd/carwings@latest

Run `carwings` by itself to see full usage information.

To update vehicle information:

    carwings -username <username> -password <password> -region <region code> update

To get latest battery status:

    carwings -username <username> -password <password> -region <region code> battery

This will print something like:

    Logging into Carwings...
    Getting latest retrieved battery status...
    Battery status as of 2017-08-06 15:43:00 -0400 EDT:
      Capacity: 240 / 240 (92%)
      Crusing range: 114 miles (107 mi with AC)
      Plug-in state: not connected
      Charging status: not charging
      Time to full:
        Level 1 charge: 8h30m0s
        Level 2 charge: 3h0m0s
        Level 2 at 6 kW: 2h30m0s

For some people the username is an email address.  For others it's a
distinct username.

The regions are:

| Nissan Region | Carwings Region Code |
| ------------- | -------------------- |
| USA           | NNA                  |
| Europe        | NE                   |
| Canada        | NCI                  |
| Australia     | NMA                  |
| Japan         | NML                  |

Config values can be provided through environment variables (such as
`CARWINGS_USERNAME`) or in a `~/.carwings` file in the format:

```
username <username>
password <password>
region NA
```

## Server mode

When `carwings server` is run, an HTTP server is started with endpoints
for retrieving battery and climate info, and for starting charging and
toggling the climate control system.  This can be handy for building
home automation workflows.  I use this with [IFTTT](https://ifttt.com)
so that I can use the Google Assistant to start warming up my car.

The endpoints are:

```
GET /battery
GET /climate
POST /charging/on
POST /climate/on
POST /climate/off
```

The `POST` endpoints take no request body.

## Carwings protocol

Josh Perry's [protocol reference](https://github.com/joshperry/carwings/blob/master/protocol.markdown)
was incredibly helpful for the development of this library.

Josh also has an implementation in Javascript:
https://github.com/joshperry/carwings

Jason Horne has a Python implementation:
https://github.com/jdhorne/pycarwings2

Phil Cole has taken it, ported it to Python 3, and is carrying it
forward: https://github.com/filcole/pycarwings2

Guillaume Boudreau has a PHP implementation:
https://github.com/gboudreau/nissan-connect-php

Scott Helme created a Javascript Alexa skill:
https://github.com/ScottHelme/AlexaNissanLeaf

Tobias Westergaard Kjeldsen has created a [Dart library for
carwings](https://gitlab.com/tobiaswkjeldsen/dartcarwings) as well as
[an Android app](https://gitlab.com/tobiaswkjeldsen/carwingsflutter).

A new North America-only API is being tracked in [issue
#3](https://github.com/joeshaw/carwings/issues/3), and more
information is available there.

## Contributing

Issues and pull requests are welcome.  When filing a PR, please make
sure the code has been run through `gofmt`.

## License

Copyright 2017-2020 Joe Shaw

`carwings` is licensed under the MIT License.  See the LICENSE file
for details.
