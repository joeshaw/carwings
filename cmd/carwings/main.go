package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lazzurs/carwings"
	"github.com/peterbourgon/ff"
)

type config struct {
	units                string
	effunits             string
	timeout              time.Duration
	serverUpdateInterval time.Duration
	serverAddr           string
}

const (
	unitsMiles = "miles"
	unitsKM    = "km"
)

const (
	unitskWhPerMile  = "kWh/mile"
	unitskWhPerKm    = "kWh/km"
	unitskWhPer100Km = "kWh/100km"
)

func usage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "USAGE\n")
		fmt.Fprintf(os.Stderr, "  %s [flags] <command>\n", os.Args[0])
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
		fmt.Fprintf(os.Stderr, "  daily             Daily driving statistics\n")
		fmt.Fprintf(os.Stderr, "  monthly <y> <m>   Monthly driving statistics\n")
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
	fs.StringVar(&region, "region", carwings.RegionUSA, "carwings region. Defaults to US (NNA).")
	fs.StringVar(&sessionFile, "session-file", "~/.carwings-session", "carwings session file")
	fs.StringVar(&cfg.units, "units", unitsMiles, "units to use (miles or km). Defaults to miles.")
	fs.StringVar(&cfg.effunits, "effunits", unitskWhPerMile, "efficiency units to use (kWh/mile, kWh/km or kWh/100km). Defaults to kWh/mile.")
	fs.StringVar(&carwings.BaseURL, "url", carwings.BaseURL, "base carwings api endpoint to use")
	fs.DurationVar(&cfg.timeout, "timeout", 60*time.Second, "update timeout. Defaults to 60s")
	fs.DurationVar(&cfg.serverUpdateInterval, "server-update-interval", 10*time.Minute, "interval to update battery info when running a server")
	fs.StringVar(&cfg.serverAddr, "server-addr", ":8040", "address for HTTP server to listen on")
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

func efficiencyToUnits(unitsIn, unitsOut string, efficiency float64) float64 {
	const milesPerKm = 0.621371

	switch unitsIn {
	case unitskWhPerMile:
		switch unitsOut {
		case unitskWhPerMile:
			return efficiency
		case unitskWhPerKm:
			return efficiency * milesPerKm
		case unitskWhPer100Km:
			return efficiency * milesPerKm * 100
		}
		panic("should not be reached")
	case unitskWhPerKm:
		switch unitsOut {
		case unitskWhPerMile:
			return efficiency / milesPerKm
		case unitskWhPerKm:
			return efficiency
		case unitskWhPer100Km:
			return efficiency * 100
		}
		panic("should not be reached")
	case unitskWhPer100Km:
		switch unitsOut {
		case unitskWhPerMile:
			return efficiency / milesPerKm / 100
		case unitskWhPerKm:
			return efficiency / 100
		case unitskWhPer100Km:
			return efficiency
		}
		panic("should not be reached")
	}
	panic("should not be reached")
}

// waitForResult will poll using the supplied method until either success or error
func waitForResult(key string, timeout time.Duration, poll func(string) (bool, error)) error {
	// All requests take more than 3 seconds, so wait this before even trying
	time.Sleep(3 * time.Second)

	start := time.Now()
	for {
		fmt.Print("+")
		done, err := poll(key)
		if done {
			break
		}
		if time.Since(start) > timeout {
			err = fmt.Errorf("timed out waiting %v for update", timeout)
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
	return waitForResult(key, cfg.timeout, s.CheckUpdate)
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
	err = waitForResult(key, cfg.timeout, s.CheckClimateOffRequest)
	if err == nil {
		fmt.Println("Climate control turned off")
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
	err = waitForResult(key, cfg.timeout, s.CheckClimateOnRequest)

	if err == nil {
		fmt.Println("Climate control turned on")
	}
	return err
}

func runMonthly(s *carwings.Session, cfg config, args []string) error {
	fmt.Println("Sending monthly statistics request...")

	var month time.Time
	if len(args) == 0 {
		month = time.Now().Local()
	} else {
		y, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		if len(args) > 1 {
			m, err := strconv.Atoi(args[1])
			if err != nil {
				return err
			}
			month = time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
		} else {
			month = time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		}
	}

	ms, err := s.GetMonthlyStatistics(month)
	if err != nil {
		return err
	}

	fmt.Printf("Monthly Driving Statistics for %s\n", month.Format("January 2006"))
	fmt.Printf("  Driving efficiency: %.4f %s over %s in %d trips\n",
		efficiencyToUnits(ms.EfficiencyScale, cfg.effunits, ms.Total.Efficiency*1000),
		cfg.effunits, prettyUnits(cfg.units, ms.Total.MetersTravelled), ms.Total.Trips)
	fmt.Printf("  Driving cost: %.4f at a rate of %.4f/kWh for %.1f kWh => %.4f/%s\n",
		ms.ElectricityBill, ms.ElectricityRate, ms.Total.PowerConsumed, ms.ElectricityBill/metersToUnits(cfg.units, ms.Total.MetersTravelled), cfg.units)
	fmt.Println()

	for i := 0; i < len(ms.Dates); i++ {
		date := ms.Dates[i]
		var distance int
		var power float64
		for j := 0; j < len(date.Trips); j++ {
			t := date.Trips[j]
			if j == 0 {
				fmt.Printf("  Trips on %s\n", t.Started.Local().Format("2006-01-02 Monday"))
			}
			distance += t.Meters
			power += t.PowerConsumedTotal

			fmt.Printf("    %5s %6.1f %s %5.1f %s %6.1f kWh\n", t.Started.Local().Format("15:04"),
				metersToUnits(cfg.units, t.Meters), cfg.units,
				efficiencyToUnits("kWh/km", cfg.effunits, t.Efficiency),
				cfg.effunits, t.PowerConsumedTotal/1000)
		}
		if distance > 0 {
			fmt.Printf("          =======%.*s ======%.*s ==========\n",
				len(cfg.units), "====",
				len(cfg.effunits), "=========")
			efficiency := (power / metersToUnits(cfg.units, distance)) / 1000
			fmt.Printf("          %6.1f %s %5.1f %s %6.1f kWh\n\n",
				metersToUnits(cfg.units, distance), cfg.units,
				efficiencyToUnits(ms.EfficiencyScale, cfg.effunits, efficiency),
				cfg.effunits, power/1000)
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
		efficiencyToUnits(ds.EfficiencyScale, cfg.effunits, ds.Efficiency),
		cfg.effunits, strings.Repeat("*", ds.EfficiencyLevel))
	fmt.Printf("  Acceleration:     %7.1f %-10.10s %-5.5s\n",
		ds.PowerConsumedMotor, "kWh", strings.Repeat("*", ds.PowerConsumedMotorLevel))
	fmt.Printf("  Regeneration:     %7.1f %-10.10s %-5.5s\n",
		ds.PowerRegeneration, "kWh", strings.Repeat("*", ds.PowerRegenerationLevel))
	fmt.Printf("  Auxilliary usage: %7.1f %-10.10s %-5.5s\n",
		ds.PowerConsumedAUX, "Wh", strings.Repeat("*", ds.PowerConsumedAUXLevel))

	return nil
}
