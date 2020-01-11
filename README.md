# Carwings

[![GoDoc](https://godoc.org/github.com/lazzurs/carwings?status.svg)](http://godoc.org/github.com/joeshaw/carwings)

`carwings` is a Go package and command-line tool providing access to
the Nissan Leaf Carwings API.

Forced from [github.com/joeshaw/carwings/cmd/carwings](github.com/joeshaw/carwings/cmd/carwings)

Through the Carwings API you can ask your vehicle for the latest data,
see current battery and charging statuses, see the current climate
control state, start or stop climate control remotely, remotely
start charging, and retrieve the last known location of the vehicle.

## Command-line tool

The `carwings` tool can be installed with:

    go get github.com/lazzurs/carwings/cmd/carwings

Run `carwings` by itself to see full usage information.

To update vehicle information:

    carwings -username <username> -password <password> update

To get latest battery status:

    carwings -username <username> -password <password> battery

This will print something like:

    Logging into Carwings...
    Getting latest retrieved battery status...
    Battery status as of 2017-08-06 15:43:00 -0400 EDT:
      Capacity: 12 / 12 (100%)
      Crusing range: 114 miles (107 mi with AC)
      Plug-in state: not connected
      Charging status: not charging
      Time to full:
        Level 1 charge: 8h30m0s
        Level 2 charge: 3h0m0s
        Level 2 at 6 kW: 2h30m0s

For some people the username is an email address.  For others it's a
distinct username.

Config values can be provided through environment variables (such as
`CARWINGS_USERNAME`) or in a `~/.carwings` file in the format:

```
username <username>
password <password>
region NA
units km
```

## Carwings protocol

There is a new protocol and other tools are dealing with that. I have forked this proect to keep in in line with the 
old protocol, even as Nissan appear to remove features.

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


## Contributing

Issues and pull requests are welcome.  When filing a PR, please make
sure the code has been run through `gofmt`.

## License

Copyright 2017-2019 Joe Shaw
Copyright 2020 Rob Lazzurs

`carwings` is licensed under the MIT License.  See the LICENSE file
for details.
