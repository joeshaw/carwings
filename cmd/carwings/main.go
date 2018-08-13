package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/joeshaw/carwings"
	"os"
	"regexp"
	"strings"
	"time"
)

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
	fmt.Fprintf(os.Stderr, "  monthly           Monthly driving statistics\n")
	fmt.Fprintf(os.Stderr, "  daily             Daily driving statistics\n")
	fmt.Fprintf(os.Stderr, "  server            Listen for requests on port 8040\n")
	fmt.Fprintf(os.Stderr, "\n")
}

func main() {
	var cfg carwings.Config
	var configFile string

	flag.StringVar(&cfg.Username, "username", "", "carwings username")
	flag.StringVar(&cfg.Password, "password", "", "carwings password")
	flag.StringVar(&cfg.Region, "region", carwings.RegionUSA, "carwings region")
	flag.StringVar(&cfg.TimeZone, "timezone", "", "carwings timezone")
	flag.StringVar(&configFile, "config", "~/.carwingsrc", "configuration filename")
	flag.BoolVar(&carwings.Debug, "debug", false, "debug mode")
	flag.Usage = usage
	flag.Parse()

	err := loadConfig(configFile, &cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	if v := os.Getenv("CARWINGS_EMAIL"); v != "" && cfg.Username == "" {
		cfg.Username = v
	}

	if v := os.Getenv("CARWINGS_PASSWORD"); v != "" && cfg.Password == "" {
		cfg.Password = v
	}

	if v := os.Getenv("CARWINGS_REGION"); v != "" && cfg.Region == "" {
		cfg.Region = v
	}

	// Allow use of some more intuitive region names and translate to CarWings regions
	switch strings.ToLower(cfg.Region) {
	case "eu":
		cfg.Region = carwings.RegionEurope
	case "au":
		cfg.Region = carwings.RegionAustralia
	case "us":
		cfg.Region = carwings.RegionUSA
	case "jp":
		cfg.Region = carwings.RegionJapan
	case "ca":
		cfg.Region = carwings.RegionCanada
	}

	if cfg.Username == "" {
		fmt.Fprintf(os.Stderr, "ERROR: -email must be provided\n")
		os.Exit(1)
	}

	if cfg.Password == "" {
		fmt.Fprintf(os.Stderr, "ERROR: -password must be provided\n")
		os.Exit(1)
	}

	var run func(*carwings.Session, []string) error

	args := flag.Args()
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
		usage()
		os.Exit(1)
	}

	s, err := carwings.Connect(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	if err := run(s, args); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

// loadConfig will load a config file like:
//   # this is a carwings configuration file
//   username=andrew@somedomain.net
//   password=5upr3m31y53cr3t
//   region=eu|us|jp|au|ca
//   units=miles|km
//   sessionfile=~/.carwings_session
// It is _NOT_ an error for the named file to be missing
func loadConfig(filename string, cfg *carwings.Config) error {
	if filename == "" {
		return nil
	}
	if filename[0] == '~' {
		filename = os.Getenv("HOME") + filename[1:]
	}
	fi, err := os.Stat(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to stat '%s': %s", filename, err.Error())
		return nil
	}
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	buff := make([]byte, fi.Size())
	f.Read(buff)
	f.Close()
	str := string(buff)
	if !strings.HasSuffix(str, "\n") {
		return errors.New("Config file does not end with a newline character.")
	}
	re := regexp.MustCompile("[#].*\\n|\\s+\\n|\\S+[=]|.*\n")
	s2 := re.FindAllString(str, -1)

	for i := 0; i < len(s2); {
		if strings.HasPrefix(s2[i], "#") {
			i++
		} else if strings.HasSuffix(s2[i], "=") {
			key := strings.ToLower(s2[i])[0 : len(s2[i])-1]
			i++
			if strings.HasSuffix(s2[i], "\n") {
				val := s2[i][0 : len(s2[i])-1]
				if strings.HasSuffix(val, "\r") {
					val = val[0 : len(val)-1]
				}
				i++

				switch strings.ToLower(key) {
				case "username":
					cfg.Username = val
				case "password":
					cfg.Password = val
				case "region":
					cfg.Region = val
				case "timezone":
					cfg.TimeZone = val
				case "units":
					if val == "miles" {
						cfg.SiUnits = false
					} else {
						cfg.SiUnits = true
					}
				case "sessionfile":
					cfg.SessionFile = val
				}
			}
		} else if strings.Index(" \t\r\n", s2[i][0:1]) > -1 {
			i++
		} else {
			return errors.New("Unable to process line in cfg file containing " + s2[i])
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

	fmt.Print("Waiting for update to complete... ")
	return s.WaitForResult(key, s.CheckUpdate)
}

func runBattery(s *carwings.Session, args []string) error {
	fmt.Println("Getting latest retrieved battery status...")

	bs, err := s.BatteryStatus()
	if err != nil {
		return err
	}

	fmt.Printf("Battery status as of %s:\n", bs.Timestamp)
	fmt.Printf("  Capacity: %d / %d (%d%%)\n", bs.Remaining, bs.Capacity, bs.StateOfCharge)
	fmt.Printf("  Cruising range: %.1f %s (%.1f %s with AC)\n",
		s.MetersToUnits(bs.CruisingRangeACOff), s.UnitsName(), s.MetersToUnits(bs.CruisingRangeACOn), s.UnitsName())
	fmt.Printf("  Plug-in state: %s\n", bs.PluginState)
	fmt.Printf("  Charging status: %s\n", bs.ChargingStatus)
	fmt.Printf("  Time to full:\n")
	if bs.TimeToFull.Level1 > 0 {
		fmt.Printf("    Level 1 charge: %s\n", bs.TimeToFull.Level1)
	}
	fmt.Printf("    Level 2 charge: %s\n", bs.TimeToFull.Level2)
	if bs.TimeToFull.Level2At6kW > 0 {
		fmt.Printf("    Level 2 at 6 kW: %s\n", bs.TimeToFull.Level2At6kW)
	}
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
	fmt.Printf("  Cruising range: %.1f %s (%.1f %s with AC)\n",
		s.MetersToUnits(cs.CruisingRangeACOff), s.UnitsName(), s.MetersToUnits(cs.CruisingRangeACOn), s.UnitsName())
	fmt.Println()

	return nil
}

func runClimateOff(s *carwings.Session, args []string) error {
	fmt.Println("Sending climate control off request...")

	key, err := s.ClimateOffRequest()
	if err != nil {
		return err
	}

	fmt.Print("Waiting for climate control update to complete... ")
	err = s.WaitForResult(key, s.CheckClimateOffRequest)
	if err == nil {
		fmt.Println("Climate control turned on")
	}
	return err
}

func runClimateOn(s *carwings.Session, args []string) error {
	fmt.Println("Sending climate control on request...")

	key, err := s.ClimateOnRequest()
	if err != nil {
		return err
	}

	fmt.Print("Waiting for climate control update to complete... ")
	err = s.WaitForResult(key, s.CheckClimateOnRequest)

	if err == nil {
		fmt.Println("Climate control turned on")
	}
	return err
}

func runLocate(s *carwings.Session, args []string) error {
	fmt.Println("Sending locate request...")

	key, err := s.LocateRequest()
	if err != nil {
		return err
	}

	fmt.Print("Waiting for location update to complete... ")
	err = s.WaitForResult(key, s.CheckLocateRequest)
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

func runMonthly(s *carwings.Session, args []string) error {
	fmt.Println("Sending monthly statistics request...")

	ms, err := s.GetMonthlyStatistics(time.Now().Local())
	if err != nil {
		return err
	}

	fmt.Println("Monthly Driving Statistics for ", time.Now().Local().Format("January 2006"))
	fmt.Printf("  Driving efficiency: %.4f %s over %.1f %s in %d trips\n",
		ms.Total.Efficiency*1000, ms.EfficiencyScale, s.MetersToUnits(ms.Total.MetersTravelled), s.UnitsName(), ms.Total.Trips)
	fmt.Printf("  Driving cost: %.4f at a rate of %.4f/kWh for %.1fkWh => %.4f/%s\n",
		ms.ElectricityBill, ms.ElectricityRate, ms.Total.PowerConsumed, ms.ElectricityBill/s.MetersToUnits(ms.Total.MetersTravelled), s.UnitsName())
	fmt.Println("")

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

			fmt.Printf("    %5s %5.1f%s %5.1f %-10.10s\n", t.Started.Local().Format("15:04"),
				s.MetersToUnits(t.Meters), s.UnitsName(), t.Efficiency, ms.EfficiencyScale)
		}
		if distance > 0 {
			fmt.Println("          =======  =======")
			efficiency := (power / s.MetersToUnits(distance)) / 10
			fmt.Printf("          %5.1f%s %5.1f %-10.10s\n\n",
				s.MetersToUnits(distance), s.UnitsName(), efficiency, ms.EfficiencyScale)
		}
	}

	return nil
}

func runDaily(s *carwings.Session, args []string) error {
	fmt.Println("Sending daily statistics request...")

	ds, err := s.GetDailyStatistics(time.Now().Local())
	if err != nil {
		return err
	}

	fmt.Println("Daily Driving Statistics for ", ds.TargetDate.Format("2006-01-02"))
	fmt.Printf("  Driving efficiency: %5.1f %-10.10s %-5.5s\n",
		ds.Efficiency, ds.EfficiencyScale, strings.Repeat("*", ds.EfficiencyLevel))
	fmt.Printf("  Acceleration:     %7.1f %-10.10s %-5.5s\n",
		ds.PowerConsumedMotor, "kWh", strings.Repeat("*", ds.PowerConsumedMotorLevel))
	fmt.Printf("  Regeneration:     %7.1f %-10.10s %-5.5s\n",
		ds.PowerRegeneration, "kWh", strings.Repeat("*", ds.PowerRegenerationLevel))
	fmt.Printf("  Auxilliary usage: %7.1f %-10.10s %-5.5s\n",
		ds.PowerConsumedAUX, "Wh", strings.Repeat("*", ds.PowerConsumedAUXLevel))

	return nil
}
