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

type config struct {
	units string
}

const (
	unitsMiles = "miles"
	unitsKM    = "km"
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
		fmt.Fprintf(os.Stderr, "  daily             Daily driving statistics\n")
		fmt.Fprintf(os.Stderr, "  monthly           Monthly driving statistics\n")
		fmt.Fprintf(os.Stderr, "  server            Listen for requests on port 8040\n")
		fmt.Fprintf(os.Stderr, "\n")
	}
}

func main() {
	var (
		cfg                 config
		username, password  string
		region, sessionFile string
	)

	fs := flag.NewFlagSet("carwings", flag.ExitOnError)
	fs.StringVar(&username, "username", "", "carwings username")
	fs.StringVar(&password, "password", "", "carwings password")
	fs.StringVar(&region, "region", carwings.RegionUSA, "carwings region")
	fs.StringVar(&sessionFile, "session-file", "~/.carwings-session", "carwings session file")
	fs.StringVar(&cfg.units, "units", unitsMiles, "units to use (miles or km)")
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

	if cfg.units != unitsMiles && cfg.units != unitsKM {
		fmt.Fprintf(os.Stderr, "ERROR: unsupported units (%q) -- must be miles or km\n", cfg.units)
		os.Exit(1)
	}

	var run func(*carwings.Session, config, []string) error

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

	case "monthly":
		run = runMonthly

	case "daily":
		run = runDaily

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

	if err := run(s, cfg, args); err != nil {
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

func prettyUnits(units string, meters int) string {
	switch units {
	case unitsMiles:
		const milesPerMeter = 0.000621371
		miles := int(float64(meters) * milesPerMeter)
		return fmt.Sprintf("%d miles", miles)

	case unitsKM:
		return fmt.Sprintf("%d km", meters/1000)
	}

	panic("should not be reached")
}

func metersToUnits(units string, meters int) float64 {
	switch units {
	case unitsMiles:
		const milesPerMeter = 0.000621371
		return float64(meters) * milesPerMeter

	case unitsKM:
		return float64(meters) / 1000
	}

	panic("should not be reached")
}

// waitForResult will poll using the supplied method until either success or error
func waitForResult(key string, poll func(string) (bool, error)) error {
	// All requests take more than 3 seconds, so wait this before even trying
	time.Sleep(3 * time.Second)

	start := time.Now()
	for {
		fmt.Print("+")
		done, err := poll(key)
		if done {
			break
		}
		if time.Since(start) > time.Minute {
			err = errors.New("timed out waiting for update")
		}
		if err != nil {
			fmt.Println("! :-(")
			return err
		}
		time.Sleep(3 * time.Second)
	}

	fmt.Println(" :-)")
	return nil
}

func runUpdate(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Requesting update from Carwings...")

	key, err := s.UpdateStatus()
	if err != nil {
		return err
	}

	fmt.Print("Waiting for update to complete... ")
	return waitForResult(key, s.CheckUpdate)
}

func runBattery(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Getting latest retrieved battery status...")

	bs, err := s.BatteryStatus()
	if err != nil {
		return err
	}

	fmt.Printf("Battery status as of %s:\n", bs.Timestamp)
	fmt.Printf("  Capacity: %d / %d (%d%%)\n", bs.Remaining, bs.Capacity, bs.StateOfCharge)
	fmt.Printf("  Cruising range: %s (%s with AC)\n", prettyUnits(cfg.units, bs.CruisingRangeACOff), prettyUnits(cfg.units, bs.CruisingRangeACOn))
	fmt.Printf("  Plug-in state: %s\n", bs.PluginState)
	fmt.Printf("  Charging status: %s\n", bs.ChargingStatus)
	fmt.Printf("  Time to full:\n")
	if bs.TimeToFull.Level1 > 0 {
		fmt.Printf("    Level 1 charge: %s\n", bs.TimeToFull.Level1)
	}
	if bs.TimeToFull.Level2 > 0 {
		fmt.Printf("    Level 2 charge: %s\n", bs.TimeToFull.Level2)
	}
	if bs.TimeToFull.Level2At6kW > 0 {
		fmt.Printf("    Level 2 at 6 kW: %s\n", bs.TimeToFull.Level2At6kW)
	}
	if bs.TimeToFull.Level1 == 0 && bs.TimeToFull.Level2 == 0 && bs.TimeToFull.Level2At6kW == 0 {
		fmt.Printf("    (no time-to-full estimates available)\n")
	}
	fmt.Println()

	return nil
}

func runCharge(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Sending charging request...")

	err := s.ChargingRequest()
	if err != nil {
		return err
	}

	fmt.Println("Charging request sent")

	return nil
}

func runClimateStatus(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Getting latest retrieved climate control status...")

	cs, err := s.ClimateControlStatus()
	if err != nil {
		return err
	}

	running := "no"
	if cs.Running {
		running = "yes"
	}

	fmt.Printf("Climate status:\n")
	fmt.Printf("  Running: %s\n", running)
	if cs.Running {
		fmt.Printf("  Will stop at: %s\n", cs.ACStopTime)
	}
	if cs.PluginState != "" {
		fmt.Printf("  Plug-in state: %s\n", cs.PluginState)
	}
	if cs.Temperature != 0 {
		fmt.Printf("  Temperature setting: %d %s\n", cs.Temperature, cs.TemperatureUnit)
	}
	fmt.Printf("  Cruising range: %s (%s with AC)\n", prettyUnits(cfg.units, cs.CruisingRangeACOff), prettyUnits(cfg.units, cs.CruisingRangeACOn))
	fmt.Println()

	return nil
}

func runClimateOff(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Sending climate control off request...")

	key, err := s.ClimateOffRequest()
	if err != nil {
		return err
	}

	fmt.Print("Waiting for climate control update to complete... ")
	err = waitForResult(key, s.CheckClimateOffRequest)
	if err == nil {
		fmt.Println("Climate control turned on")
	}
	return err
}

func runClimateOn(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Sending climate control on request...")

	key, err := s.ClimateOnRequest()
	if err != nil {
		return err
	}

	fmt.Print("Waiting for climate control update to complete... ")
	err = waitForResult(key, s.CheckClimateOnRequest)

	if err == nil {
		fmt.Println("Climate control turned on")
	}
	return err
}

func runLocate(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Sending locate request...")

	key, err := s.LocateRequest()
	if err != nil {
		return err
	}

	fmt.Print("Waiting for location update to complete... ")
	err = waitForResult(key, s.CheckLocateRequest)
	if err != nil {
		return err
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

func runMonthly(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Sending monthly statistics request...")

	ms, err := s.GetMonthlyStatistics(time.Now().Local())
	if err != nil {
		return err
	}

	efficiency := (ms.Total.PowerConsumed / metersToUnits(cfg.units, ms.Total.MetersTravelled)) / 10

	fmt.Println("Monthly Driving Statistics for ", time.Now().Local().Format("January 2006"))
	fmt.Printf("  Driving efficiency: %.1f %s over %s in %d trips\n",
		efficiency, ms.EfficiencyScale, prettyUnits(cfg.units, ms.Total.MetersTravelled), ms.Total.Trips)
	fmt.Println(ms.Total)

	for i := 0; i < len(ms.Dates); i++ {
		date := ms.Dates[i]
		var distance int
		var power float64
		for j := 0; j < len(date.Trips); j++ {
			if j == 0 {
				fmt.Printf("  Trips on %s\n", date.TargetDate)
			}
			t := date.Trips[j]
			distance += t.Meters
			power += t.PowerConsumedTotal

			fmt.Printf("    %5s %s %5.1f %-10.10s\n", t.Started.Local().Format("15:04"),
				prettyUnits(cfg.units, t.Meters), t.Efficiency, ms.EfficiencyScale)
		}
		if distance > 0 {
			fmt.Println("          =======  =======")
			efficiency = (power / metersToUnits(cfg.units, distance)) / 10
			fmt.Printf("          %s %5.1f %-10.10s\n\n",
				prettyUnits(cfg.units, distance), efficiency, ms.EfficiencyScale)
		}
	}

	return nil
}

func runDaily(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Sending daily statistics request...")

	ds, err := s.GetDailyStatistics(time.Now().Local())
	if err != nil {
		return err
	}

	fmt.Printf("Daily Driving Statistics for %s\n", ds.TargetDate.Format("2006-01-02"))
	fmt.Printf("  Driving efficiency: %5.1f %-10.10s %-5.5s\n",
		ds.Efficiency, ds.EfficiencyScale, strings.Repeat("*", ds.EfficiencyLevel))
	fmt.Printf("  Acceleration:     %7.1f %-10.10s %-5.5s\n",
		ds.PowerConsumeMotor, "kWh", strings.Repeat("*", ds.PowerConsumeMotorLevel))
	fmt.Printf("  Regeneration:     %7.1f %-10.10s %-5.5s\n",
		ds.PowerRegeneration, "kWh", strings.Repeat("*", ds.PowerRegenerationLevel))
	fmt.Printf("  Auxilliary usage: %7.1f %-10.10s %-5.5s\n",
		ds.PowerConsumeAUX, "Wh", strings.Repeat("*", ds.PowerConsumeAUXLevel))

	return nil
}
