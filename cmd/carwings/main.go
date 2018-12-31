package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joeshaw/carwings"
	"github.com/peterbourgon/ff"
)

func usage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "USAGE\n")
		fmt.Fprintf(os.Stderr, "  %s <mode> [flags]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "FLAGS\n")
		fs.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(os.Stderr, "  -%s %s\n", f.Name, f.Usage)
		})
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "COMMANDS\n")
		fmt.Fprintf(os.Stderr, "  update            Load latest data from vehicle\n")
		fmt.Fprintf(os.Stderr, "  battery           Get most recently loaded battery status\n")
		fmt.Fprintf(os.Stderr, "  charge            Begin charging plugged-in vehicle\n")
		fmt.Fprintf(os.Stderr, "  climate           Get most recently loaded climate control status\n")
		fmt.Fprintf(os.Stderr, "  climate-off       Turn off climate control\n")
		fmt.Fprintf(os.Stderr, "  climate-on        Turn on climate control\n")
		fmt.Fprintf(os.Stderr, "  locate            Locate vehicle\n")
		fmt.Fprintf(os.Stderr, "\n")
	}
}

func main() {
	var (
		username, password  string
		region, sessionFile string
	)

	fs := flag.NewFlagSet("carwings", flag.ExitOnError)
	fs.StringVar(&username, "username", "", "carwings username")
	fs.StringVar(&password, "password", "", "carwings password")
	fs.StringVar(&region, "region", carwings.RegionUSA, "carwings region")
	fs.StringVar(&sessionFile, "session-file", "~/.carwings-session", "carwings session file")
	fs.BoolVar(&carwings.Debug, "debug", false, "debug mode")
	fs.Usage = usage(fs)

	ff.Parse(fs, os.Args[1:],
		ff.WithConfigFile(filepath.Join(os.Getenv("HOME"), ".carwings")),
		ff.WithConfigFileParser(configParser),
		ff.WithEnvVarPrefix("CARWINGS"),
	)

	args := fs.Args()
	if len(args) < 1 {
		fs.Usage()
		os.Exit(1)
	}

	if username == "" {
		fmt.Fprintf(os.Stderr, "ERROR: -username must be provided (it used to be -email)\n")
		os.Exit(1)
	}

	if password == "" {
		fmt.Fprintf(os.Stderr, "ERROR: -password must be provided\n")
		os.Exit(1)
	}

	var run func(*carwings.Session, []string) error

	cmd, args := strings.ToLower(args[0]), args[1:]
	switch cmd {
	case "update":
		run = runUpdate

	case "battery":
		run = runBattery

	case "charge":
		run = runCharge

	case "climate":
		run = runClimateStatus

	case "climate-off":
		run = runClimateOff

	case "climate-on":
		run = runClimateOn

	case "locate":
		run = runLocate

	case "server":
		run = runServer

	default:
		fs.Usage()
		os.Exit(1)
	}

	fmt.Println("Logging into Carwings...")

	s := &carwings.Session{
		Region:   region,
		Filename: sessionFile,
	}

	if err := s.Connect(username, password); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	if err := run(s, args); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func configParser(r io.Reader, set func(name, value string) error) error {
	// This is a copy of ff.PlainParser() with two differences:
	// 1. This strips trailing colons from the names, to maintain
	//    backward compatibility with the old config file format
	// 2. This ignores intra-line # symbols, which PlainParser
	//    interprets as comments and strips.  This caused problems
	//    with passwords that included them.
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue // skip empties
		}

		if line[0] == '#' {
			continue // skip comments
		}

		var (
			name  string
			value string
			index = strings.IndexRune(line, ' ')
		)
		if index < 0 {
			name, value = line, "true" // boolean option
		} else {
			name, value = line[:index], strings.TrimSpace(line[index:])
		}

		if strings.HasSuffix(name, ":") {
			name = name[:len(name)-1]
		}

		if err := set(name, value); err != nil {
			return err
		}
	}
	return nil
}

func runUpdate(s *carwings.Session, args []string) error {
	fmt.Println("Requesting update from Carwings...")

	key, err := s.UpdateStatus()
	if err != nil {
		return err
	}

	start := time.Now()
	for {
		fmt.Println("Checking if update finished...")
		done, err := s.CheckUpdate(key)
		if err != nil {
			return err
		}
		if done {
			break
		}
		if time.Since(start) > 2*time.Minute {
			return errors.New("timed out waiting for update")
		}
		time.Sleep(5 * time.Second)
	}

	fmt.Println("Update complete")
	return nil
}

func runBattery(s *carwings.Session, args []string) error {
	fmt.Println("Getting latest retrieved battery status...")

	bs, err := s.BatteryStatus()
	if err != nil {
		return err
	}

	fmt.Printf("Battery status as of %s:\n", bs.Timestamp)
	fmt.Printf("  Capacity: %d / %d (%d%%)\n", bs.Remaining, bs.Capacity, bs.StateOfCharge)
	fmt.Printf("  Crusing range: %d mi (%d mi with AC)\n", carwings.MetersToMiles(bs.CruisingRangeACOff), carwings.MetersToMiles(bs.CruisingRangeACOn))
	fmt.Printf("  Plug-in state: %s\n", bs.PluginState)
	fmt.Printf("  Charging status: %s\n", bs.ChargingStatus)
	fmt.Printf("  Time to full:\n")
	fmt.Printf("    Level 1 charge: %s\n", bs.TimeToFull.Level1)
	fmt.Printf("    Level 2 charge: %s\n", bs.TimeToFull.Level2)
	fmt.Printf("    Level 2 at 6 kW: %s\n", bs.TimeToFull.Level2At6kW)
	fmt.Println()

	return nil
}

func runCharge(s *carwings.Session, args []string) error {
	fmt.Println("Sending charging request...")

	err := s.ChargingRequest()
	if err != nil {
		return err
	}

	fmt.Println("Charging request sent")

	return nil
}

func runClimateStatus(s *carwings.Session, args []string) error {
	fmt.Println("Getting latest retrieved climate control status...")

	cs, err := s.ClimateControlStatus()
	if err != nil {
		return err
	}

	running := "no"
	if cs.Running {
		running = "yes"
	}

	fmt.Printf("Climate control status:\n")
	fmt.Printf("  Running: %s\n", running)
	if cs.PluginState != "" {
		fmt.Printf("  Plug-in state: %s\n", cs.PluginState)
	}
	if cs.Temperature != 0 {
		fmt.Printf("  Temperature setting: %d %s\n", cs.Temperature, cs.TemperatureUnit)
	}
	fmt.Println()

	return nil
}

func runClimateOff(s *carwings.Session, args []string) error {
	fmt.Println("Sending climate control off request...")

	key, err := s.ClimateOffRequest()
	if err != nil {
		return err
	}

	start := time.Now()
	for {
		fmt.Println("Checking if climate control update finished...")
		done, err := s.CheckClimateOffRequest(key)
		if err != nil {
			return err
		}
		if done {
			break
		}
		if time.Since(start) > 2*time.Minute {
			return errors.New("timed out waiting for update")
		}
		time.Sleep(5 * time.Second)
	}

	fmt.Println("Climate control turned off")
	return nil
}

func runClimateOn(s *carwings.Session, args []string) error {
	fmt.Println("Sending climate control on request...")

	key, err := s.ClimateOnRequest()
	if err != nil {
		return err
	}

	start := time.Now()
	for {
		fmt.Println("Checking if climate control update finished...")
		done, err := s.CheckClimateOnRequest(key)
		if err != nil {
			return err
		}
		if done {
			break
		}
		if time.Since(start) > 2*time.Minute {
			return errors.New("timed out waiting for update")
		}
		time.Sleep(5 * time.Second)
	}

	fmt.Println("Climate control turned on")
	return nil
}

func runLocate(s *carwings.Session, args []string) error {
	fmt.Println("Sending locate request...")

	key, err := s.LocateRequest()
	if err != nil {
		return err
	}

	start := time.Now()
	for {
		fmt.Println("Checking if locate request finished...")
		done, err := s.CheckLocateRequest(key)
		if err != nil {
			return err
		}
		if done {
			break
		}
		if time.Since(start) > 2*time.Minute {
			return errors.New("timed out waiting for update")
		}
		time.Sleep(5 * time.Second)
	}

	fmt.Println("Getting latest vehicle position...")

	vl, err := s.LocateVehicle()
	if err != nil {
		return err
	}

	fmt.Printf("Vehicle location as of %s:\n", vl.Timestamp)
	fmt.Printf("  Latitude: %s\n", vl.Latitude)
	fmt.Printf("  Longitude: %s\n", vl.Longitude)
	fmt.Printf("  Link: https://www.google.com/maps/place/%s,%s\n", vl.Latitude, vl.Longitude)
	fmt.Println()

	return nil
}
