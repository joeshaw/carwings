package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joeshaw/carwings"
)

type config struct {
	email    string
	password string
	region   string
}

func usage() {
	fmt.Fprintf(os.Stderr, "USAGE\n")
	fmt.Fprintf(os.Stderr, "  %s <mode> [flags]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "FLAGS\n")
	flag.VisitAll(func(f *flag.Flag) {
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

func main() {
	var cfg config

	flag.StringVar(&cfg.email, "email", "", "carwings email address")
	flag.StringVar(&cfg.password, "password", "", "carwings password")
	flag.StringVar(&cfg.region, "region", carwings.RegionUSA, "carwings region")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		usage()
		os.Exit(1)
	}

	if cfg.email == "" {
		fmt.Fprintf(os.Stderr, "ERROR: -email must be provided\n")
		os.Exit(1)
	}

	if cfg.password == "" {
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

	default:
		usage()
		os.Exit(1)
	}

	fmt.Println("Logging into Carwings...")

	s, err := carwings.Connect(cfg.email, cfg.password, cfg.region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	if err := run(s, args); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
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
	fmt.Printf("  Plug-in state: %s\n", cs.PluginState)
	fmt.Printf("  Temperature setting: %d %s\n", cs.Temperature, cs.TemperatureUnit)
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
	fmt.Println("Sending climate control off request...")

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
