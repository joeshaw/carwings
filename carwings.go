package carwings

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	//lint:ignore SA1019 Blowfish is terrible, but that's what the Nissan API uses
	"golang.org/x/crypto/blowfish"
)

const (
	initialAppStrings = "9s5rfKVuMrT03RtzajWNcA"
)

var (
	// ErrNotLoggedIn is returned whenever an operation is run and
	// the user has not let logged in.
	ErrNotLoggedIn = errors.New("not logged in")

	// ErrUpdateFailed indicates an error talking to the Carwings
	// service when fetching updated vehicle data.
	ErrUpdateFailed = errors.New("failed to retrieve updated info from vehicle")

	// ErrClimateStatusUnavailable is returned from the
	// ClimateStatus method when no data is available.
	ErrClimateStatusUnavailable = errors.New("climate status unavailable")

	// ErrBatteryStatusUnavailable is returned from the
	// BatteryStatus method when no data is available.
	ErrBatteryStatusUnavailable = errors.New("battery status unavailable")

	// ErrVehicleInfoUnavailable is returned when vehicle information is
	// not available when logging in.
	ErrVehicleInfoUnavailable = errors.New("vehicle info unavailable")

	// Debug indiciates whether to log HTTP responses to stderr
	Debug = false

	// Default URL for connecting to Carwings service.  This is
	// changed by Nissan from time to time, so it's helpful to
	// have it be configurable.
	BaseURL = "https://gdcportalgw.its-mo.com/api_v200413_NE/gdc/"

	// Http client used for api requests
	Client = http.DefaultClient
)

func pkcs5Padding(data []byte, blocksize int) []byte {
	padLen := blocksize - (len(data) % blocksize)
	padding := bytes.Repeat([]byte{byte(padLen)}, padLen)
	return append(data, padding...)
}

// Pads the source, does ECB Blowfish encryption on it, and returns a
// base64-encoded string.
func encrypt(s, key string) (string, error) {
	cipher, err := blowfish.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	src := []byte(s)
	src = pkcs5Padding(src, cipher.BlockSize())

	dst := make([]byte, len(src))
	pos := 0
	for pos < len(src) {
		cipher.Encrypt(dst[pos:], src[pos:])
		pos += cipher.BlockSize()
	}

	return base64.StdEncoding.EncodeToString(dst), nil
}

// MetersToMiles converts Carwings distances (in meters) to miles.
func MetersToMiles(meters int) int {
	const MilesPerMeter = 0.000621371
	return int(float64(meters) * MilesPerMeter)
}

const (
	RegionUSA       = "NNA"
	RegionEurope    = "NE"
	RegionCanada    = "NCI"
	RegionAustralia = "NMA"
	RegionJapan     = "NML"
)

// Session defines a one or more connections to the Carwings service
type Session struct {
	// Region is one of the predefined region codes where this car operates.
	Region string

	// Filename is an optional file to load and save an existing session to.
	Filename string

	username        string
	encpw           string
	VIN             string
	customSessionID string
	tz              string
	loc             *time.Location
	cabinTemp       int
}

// ClimateStatus contains information about the vehicle's climate
// control (AC or heater) status.
type ClimateStatus struct {
	// Date and time this status was retrieved from the vehicle.
	LastOperationTime time.Time

	// The current climate control operation status.
	Running bool

	// Current plugged-in state
	PluginState PluginState

	// The amount of time the climate control system will run
	// while on battery power, in seconds.
	BatteryDuration int

	// The amount of time the climate control system will run
	// while plugged in, in seconds.
	PluggedDuration int

	// The climate preset temperature unit, F or C
	TemperatureUnit string

	// The climate preset temperature value
	Temperature int

	// Time the AC was stopped, or is scheduled to stop
	ACStopTime time.Time

	// Estimated cruising range with climate control on, in
	// meters.
	CruisingRangeACOn int

	// Estimated cruising range with climate control off, in
	// meters.
	CruisingRangeACOff int
}

// BatteryStatus contains information about the vehicle's state of
// charge, current plugged-in state, charging status, and the time to
// charge the battery to full.
type BatteryStatus struct {
	// Date and time this battery status was retrieved from the
	// vehicle.
	Timestamp time.Time

	// Total capacity of the battery.  Units unknown.
	Capacity int

	// Remaining battery level.  Units unknown, but same as Capacity.
	Remaining int

	// Remaining battery level in Watt Hours.
	RemainingWH int

	// Current state of charge.  In percent, should be roughly
	// equivalent to Remaining / Capacity * 100.
	StateOfCharge int // percent

	// Estimated cruising range with climate control on, in
	// meters.
	CruisingRangeACOn int

	// Estimated cruising range with climate control off, in
	// meters.
	CruisingRangeACOff int

	// Current plugged-in state
	PluginState PluginState

	// Current charging status
	ChargingStatus ChargingStatus

	// Amount of time remaining until battery is fully charged,
	// using different possible charging methods.
	TimeToFull TimeToFull
}

// TimeToFull contains information about how long it will take to
// charge the battery to full via different charging methods.
type TimeToFull struct {
	// Time to fully charge the battery using a 1.4 kW Level 1
	// (120V 12A) trickle charge.
	Level1 time.Duration

	// Time to fully charge the battery using a 3.3 kW Level 2
	// (240V ~15A) charge.
	Level2 time.Duration

	// Time to fully charge the battery using a 6.6 kW Level 2
	// (240V ~30A) charge.
	Level2At6kW time.Duration
}

// VehicleLocation indicates the vehicle's current location.
type VehicleLocation struct {
	// Timestamp of the last time vehicle location was updated.
	Timestamp time.Time

	// Latitude of the vehicle
	Latitude string

	// Longitude of the vehicle
	Longitude string
}

// PluginState indicates whether and how the vehicle is plugged in.
// It is separate from ChargingStatus, because the vehicle can be
// plugged in but not actively charging.
type PluginState string

const (
	// Not connected to a charger
	NotConnected = PluginState("NOT_CONNECTED")

	// Connected to a normal J1772 Level 1 or 2 charger
	Connected = PluginState("CONNECTED")

	// Connected to a high voltage DC quick charger (ChaDeMo)
	QCConnected = PluginState("QC_CONNECTED")

	// Invalid state, when updating data from the vehicle fails.
	InvalidPluginState = PluginState("INVALID")
)

func (ps PluginState) String() string {
	switch ps {
	case NotConnected:
		return "not connected"
	case Connected:
		return "connected"
	case QCConnected:
		return "connected to quick charger"
	case InvalidPluginState:
		return "invalid"
	default:
		return string(ps)
	}
}

// ChargingStatus indicates whether and how the vehicle is charging.
type ChargingStatus string

const (
	// Not charging
	NotCharging = ChargingStatus("NOT_CHARGING")

	// Normal charging from a Level 1 or 2 EVSE
	NormalCharging = ChargingStatus("NORMAL_CHARGING")

	// Rapidly charging from a ChaDeMo DC quick charger
	RapidlyCharging = ChargingStatus("RAPIDLY_CHARGING")

	// Invalid state, when updating data from the vehicle fails.
	InvalidChargingStatus = ChargingStatus("INVALID")
)

func (cs ChargingStatus) String() string {
	switch cs {
	case NotCharging:
		return "not charging"
	case NormalCharging:
		return "charging"
	case RapidlyCharging:
		return "rapidly charging"
	case InvalidChargingStatus:
		return "invalid"
	default:
		return string(cs)
	}
}

// OperationResult
const (
	start                = "START"
	electricWaveAbnormal = "ELECTRIC_WAVE_ABNORMAL"
)

type cwTime time.Time

func (cwt *cwTime) UnmarshalJSON(data []byte) error {
	if data == nil || string(data) == `""` {
		return nil
	}

	// Carwings uses at least five different date formats! 🙄🙄🙄
	t, err := time.Parse(`"2006\/01\/02 15:04"`, string(data))
	if err == nil {
		*cwt = cwTime(t)
		return nil
	}

	t, err = time.Parse(`"2006-01-02 15:04:05"`, string(data))
	if err == nil {
		*cwt = cwTime(t)
		return nil
	}

	// Also e.g. "UserVehicleBoundTime": "2018-08-04T15:08:33Z"
	t, err = time.Parse(`"2006-01-02T15:04:05Z"`, string(data))
	if err == nil {
		*cwt = cwTime(t)
		return nil
	}

	// Also e.g. "GpsDatetime": "2018-08-05T10:18:47" in monthly statistics response
	t, err = time.Parse(`"2006-01-02T15:04:05"`, string(data))
	if err == nil {
		*cwt = cwTime(t)
		return nil
	}

	// Also e.g. "LastScheduledTime": "2018-08-04T15:08:33Z" in ClimateControlSchedule response
	t, err = time.Parse(`"Jan _2, 2006 03:04 PM"`, string(data))
	if err == nil {
		*cwt = cwTime(t)
		return nil
	}

	return fmt.Errorf("cannot parse %q as carwings time", string(data))
}

// FixLocation alters the location associated with the time, without changing
// the value.  This is needed since all times are parsed as if they were UTC
// when in fact some of them are in the timezone specified in the session.
func (cwt cwTime) FixLocation(location *time.Location) cwTime {
	t := time.Time(cwt)
	return cwTime(time.Date(
		t.Year(),
		t.Month(),
		t.Day(),
		t.Hour(),
		t.Minute(),
		t.Second(),
		t.Nanosecond(),
		location,
	))
}

type response interface {
	Status() int
	ErrorMessage() string
}

type baseResponse struct {
	StatusCode json.RawMessage `json:"status"`
	Message    string          `json:"message"`
}

func (r *baseResponse) Status() int {
	s := r.StatusCode
	if s[0] == '"' {
		s = s[1 : len(s)-1]
	}

	v, _ := strconv.Atoi(string(s))
	return v
}

func (r *baseResponse) ErrorMessage() string {
	return r.Message
}

func apiRequest(endpoint string, params url.Values, target response) error {
	req, err := http.NewRequest("POST", BaseURL+endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "")

	if Debug {
		body, err := httputil.DumpRequestOut(req, true)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(os.Stderr, string(body))
		fmt.Fprintln(os.Stderr)
	}

	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if Debug {
		body, err := httputil.DumpResponse(resp, true)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(os.Stderr, string(body))
		fmt.Fprintln(os.Stderr)
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(target); err != nil {
		return err
	}

	switch s := target.Status(); s {
	case http.StatusOK:
		return nil

	case http.StatusUnauthorized, http.StatusRequestTimeout:
		return ErrNotLoggedIn

	default:
		if e := target.ErrorMessage(); e != "" {
			return fmt.Errorf("received status code %d (%s)", s, e)
		}
		return fmt.Errorf("received status code %d", s)
	}
}

// Connect establishes a new authenticated Session with the Carwings
// service.
func (s *Session) Connect(username, password string) error {
	params := url.Values{}
	params.Set("initial_app_str", initialAppStrings)

	var initResp struct {
		baseResponse
		Baseprm string `json:"baseprm"`
	}
	if err := apiRequest("InitialApp_v2.php", params, &initResp); err != nil {
		return err
	}

	encpw, err := encrypt(password, initResp.Baseprm)
	if err != nil {
		return err
	}

	s.username = username
	s.encpw = encpw

	if s.Filename != "" {
		if err := s.load(); err == nil {
			return nil
		} else if Debug {
			fmt.Fprintf(os.Stderr, "Error loading session from %s: %v\n", s.Filename, err)
		}
	}

	return s.Login()
}

func (s *Session) Login() error {
	params := url.Values{}
	params.Set("initial_app_str", initialAppStrings)

	params.Set("UserId", s.username)
	params.Set("Password", s.encpw)
	params.Set("RegionCode", s.Region)

	// Not a comprehensive representation, just what we need
	type vehicleInfo struct {
		VIN             string `json:"vin"`
		CustomSessionID string `json:"custom_sessionid"`
	}

	var loginResp struct {
		baseResponse

		// OMG this API... one of these three will be populated.
		VehicleInfos    []vehicleInfo `json:"vehicleInfo"`
		VehicleInfoList struct {
			VehicleInfos []vehicleInfo `json:"vehicleInfo"`
		} `json:"vehicleInfoList"`
		VehicleInfo vehicleInfo `json:"VehicleInfo"`

		CustomerInfo struct {
			Timezone    string
			VehicleInfo vehicleInfo `json:"VehicleInfo"`
		}
	}
	if err := apiRequest("UserLoginRequest.php", params, &loginResp); err != nil {
		return err
	}

	var vi vehicleInfo
	switch {
	case len(loginResp.VehicleInfos) > 0:
		vi = loginResp.VehicleInfos[0]

	case len(loginResp.VehicleInfoList.VehicleInfos) > 0:
		vi = loginResp.VehicleInfoList.VehicleInfos[0]

	case len(loginResp.CustomerInfo.VehicleInfo.VIN) > 0:
		vi = loginResp.CustomerInfo.VehicleInfo

	default:
		vi = loginResp.VehicleInfo
	}

	if vi.VIN == "" {
		return ErrVehicleInfoUnavailable
	}

	s.customSessionID = vi.CustomSessionID
	s.VIN = vi.VIN
	s.tz = loginResp.CustomerInfo.Timezone

	loc, err := time.LoadLocation(loginResp.CustomerInfo.Timezone)
	if err != nil {
		loc = time.UTC
	}
	s.loc = loc

	if s.Filename != "" {
		return s.save()
	}

	return nil
}

func (s *Session) load() error {
	if s.Filename[0] == '~' {
		s.Filename = os.Getenv("HOME") + s.Filename[1:]
	}

	f, err := os.Open(s.Filename)
	if err != nil {
		return err
	}
	defer f.Close()

	m := map[string]string{}
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return err
	}

	s.VIN = m["vin"]
	s.customSessionID = m["customSessionID"]
	s.tz = m["tz"]

	loc, err := time.LoadLocation(m["tz"])
	if err != nil {
		loc = time.UTC
	}
	s.loc = loc

	return nil
}

func (s *Session) save() error {
	if s.Filename[0] == '~' {
		s.Filename = os.Getenv("HOME") + s.Filename[1:]
	}

	f, err := os.OpenFile(s.Filename, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	m := map[string]string{
		"vin":             s.VIN,
		"customSessionID": s.customSessionID,
		"tz":              s.tz,
	}

	if err := json.NewEncoder(f).Encode(m); err != nil {
		f.Close()
		os.Remove(s.Filename)
		return err
	}

	return f.Close()
}

func (s *Session) apiRequest(endpoint string, params url.Values, target response) error {
	params = s.setCommonParams(params)

	err := apiRequest(endpoint, params, target)
	if err == ErrNotLoggedIn {
		if err := s.Login(); err != nil {
			return err
		}

		params = s.setCommonParams(params)
		return apiRequest(endpoint, params, target)
	}

	return err
}

func (s *Session) setCommonParams(params url.Values) url.Values {
	if params == nil {
		params = url.Values{}
	}

	params.Set("RegionCode", s.Region)
	params.Set("VIN", s.VIN)
	params.Set("custom_sessionid", s.customSessionID)
	params.Set("tz", s.tz)
	return params
}

// UpdateStatus asks the Carwings service to request an update from
// the vehicle.  This is an asynchronous operation: it returns a
// "result key" that must be used to poll for status with the
// CheckUpdate method.
func (s *Session) UpdateStatus() (string, error) {
	var resp struct {
		baseResponse
		ResultKey string `json:"resultKey"`
	}
	if err := s.apiRequest("BatteryStatusCheckRequest.php", nil, &resp); err != nil {
		return "", err
	}

	return resp.ResultKey, nil
}

// CheckUpdate returns whether the update corresponding to the
// provided result key has finished.
func (s *Session) CheckUpdate(resultKey string) (bool, error) {
	params := url.Values{}
	params.Set("resultKey", resultKey)

	var resp struct {
		baseResponse
		ResponseFlag    int    `json:"responseFlag,string"`
		OperationResult string `json:"operationResult"`
	}

	if err := s.apiRequest("BatteryStatusCheckResultRequest.php", params, &resp); err != nil {
		return false, err
	}

	var err error
	if resp.OperationResult == electricWaveAbnormal {
		err = ErrUpdateFailed
	}

	return resp.ResponseFlag == 1, err
}

// BatteryStatus returns the most recent battery status from the
// Carwings service.  Note that this data is not real-time: it is
// cached from the last time the vehicle data was updated.  Use
// UpdateStatus method to update vehicle data.
func (s *Session) BatteryStatus() (BatteryStatus, error) {
	type batteryStatusRecord struct {
		BatteryStatus struct {
			BatteryChargingStatus     string
			BatteryCapacity           int `json:",string"`
			BatteryRemainingAmount    string
			BatteryRemainingAmountWH  string
			BatteryRemainingAmountKWH string
			SOC                       struct {
				Value int `json:",string"`
			}
		}
		PluginState        string
		CruisingRangeAcOn  json.Number `json:",string"`
		CruisingRangeAcOff json.Number `json:",string"`
		TimeRequiredToFull struct {
			HourRequiredToFull    int `json:",string"`
			MinutesRequiredToFull int `json:",string"`
		}
		TimeRequiredToFull200 struct {
			HourRequiredToFull    int `json:",string"`
			MinutesRequiredToFull int `json:",string"`
		}
		TimeRequiredToFull200_6kW struct {
			HourRequiredToFull    int `json:",string"`
			MinutesRequiredToFull int `json:",string"`
		}
		NotificationDateAndTime cwTime
	}

	var resp struct {
		baseResponse
		BatteryStatusRecords json.RawMessage
	}
	if err := s.apiRequest("BatteryStatusRecordsRequest.php", nil, &resp); err != nil {
		return BatteryStatus{}, err
	}

	if string(resp.BatteryStatusRecords) == "[]" {
		return BatteryStatus{}, ErrBatteryStatusUnavailable
	}

	var batrec batteryStatusRecord
	if err := json.Unmarshal(resp.BatteryStatusRecords, &batrec); err != nil {
		return BatteryStatus{}, err
	}

	remaining, _ := strconv.Atoi(batrec.BatteryStatus.BatteryRemainingAmount)
	remainingWH, _ := strconv.Atoi(batrec.BatteryStatus.BatteryRemainingAmountWH)
	acOn, _ := batrec.CruisingRangeAcOn.Float64()
	acOff, _ := batrec.CruisingRangeAcOff.Float64()

	soc := batrec.BatteryStatus.SOC.Value
	if soc == 0 {
		soc = int(math.Round(float64(remaining) / float64(batrec.BatteryStatus.BatteryCapacity) * 100))
	}

	bs := BatteryStatus{
		Timestamp:          time.Time(batrec.NotificationDateAndTime).In(s.loc),
		Capacity:           batrec.BatteryStatus.BatteryCapacity,
		Remaining:          remaining,
		RemainingWH:        remainingWH,
		StateOfCharge:      soc,
		CruisingRangeACOn:  int(acOn),
		CruisingRangeACOff: int(acOff),
		PluginState:        PluginState(batrec.PluginState),
		ChargingStatus:     ChargingStatus(batrec.BatteryStatus.BatteryChargingStatus),
		TimeToFull: TimeToFull{
			Level1:      time.Duration(batrec.TimeRequiredToFull.HourRequiredToFull)*time.Hour + time.Duration(batrec.TimeRequiredToFull.MinutesRequiredToFull)*time.Minute,
			Level2:      time.Duration(batrec.TimeRequiredToFull200.HourRequiredToFull)*time.Hour + time.Duration(batrec.TimeRequiredToFull200.MinutesRequiredToFull)*time.Minute,
			Level2At6kW: time.Duration(batrec.TimeRequiredToFull200_6kW.HourRequiredToFull)*time.Hour + time.Duration(batrec.TimeRequiredToFull200_6kW.MinutesRequiredToFull)*time.Minute,
		},
	}

	return bs, nil
}

// ClimateControlStatus returns the most recent climate control status
// from the Carwings service.
func (s *Session) ClimateControlStatus() (ClimateStatus, error) {
	type remoteACRecords struct {
		OperationResult        string
		OperationDateAndTime   cwTime
		RemoteACOperation      string
		ACStartStopDateAndTime cwTime
		ACStartStopURL         string
		CruisingRangeAcOn      json.Number `json:",string"`
		CruisingRangeAcOff     json.Number `json:",string"`
		PluginState            string
		ACDurationBatterySec   int `json:",string"`
		ACDurationPluggedSec   int `json:",string"`
		PreAC_unit             string
		PreAC_temp             int `json:",string"`
	}

	var resp struct {
		baseResponse
		RemoteACRecords json.RawMessage
	}

	if err := s.apiRequest("RemoteACRecordsRequest.php", nil, &resp); err != nil {
		return ClimateStatus{}, err
	}

	// Sometimes the RemoteACRecords field is an empty array
	// instead of a struct value.  This API... ¯\_(ツ)_/¯
	if string(resp.RemoteACRecords) == "[]" {
		return ClimateStatus{}, ErrClimateStatusUnavailable
	}

	var racr remoteACRecords
	if err := json.Unmarshal(resp.RemoteACRecords, &racr); err != nil {
		return ClimateStatus{}, err
	}

	acOn, _ := racr.CruisingRangeAcOn.Float64()
	acOff, _ := racr.CruisingRangeAcOff.Float64()

	running := racr.RemoteACOperation == "START"
	acStopTime := time.Time(racr.ACStartStopDateAndTime).In(s.loc)
	if running {
		if NotConnected == PluginState(racr.PluginState) {
			acStopTime = acStopTime.Add(time.Second * time.Duration(racr.ACDurationBatterySec))
		} else {
			acStopTime = acStopTime.Add(time.Second * time.Duration(racr.ACDurationPluggedSec))
		}
	}

	cs := ClimateStatus{
		LastOperationTime:  time.Time(racr.OperationDateAndTime.FixLocation(s.loc)),
		Running:            running,
		PluginState:        PluginState(racr.PluginState),
		BatteryDuration:    racr.ACDurationBatterySec,
		PluggedDuration:    racr.ACDurationPluggedSec,
		TemperatureUnit:    racr.PreAC_unit,
		Temperature:        racr.PreAC_temp,
		ACStopTime:         acStopTime,
		CruisingRangeACOn:  int(acOn),
		CruisingRangeACOff: int(acOff),
	}

	return cs, nil
}

// ClimateOffRequest sends a request to turn off the climate control
// system.  This is an asynchronous operation: it returns a "result
// key" that can be used to poll for status with the
// CheckClimateOffRequest method.
func (s *Session) ClimateOffRequest() (string, error) {
	var resp struct {
		baseResponse
		ResultKey string `json:"resultKey"`
	}

	if err := s.apiRequest("ACRemoteOffRequest.php", nil, &resp); err != nil {
		return "", err
	}

	return resp.ResultKey, nil
}

// CheckClimateOffRequest returns whether the ClimateOffRequest has
// finished.
func (s *Session) CheckClimateOffRequest(resultKey string) (bool, error) {
	var resp struct {
		baseResponse
		ResponseFlag    int    `json:"responseFlag,string"` // 0 or 1
		OperationResult string `json:"operationResult"`
		TimeStamp       cwTime `json:"timeStamp"`
		HVACStatus      string `json:"hvacStatus"`
	}

	params := url.Values{}
	params.Set("resultKey", resultKey)

	if err := s.apiRequest("ACRemoteOffResult.php", params, &resp); err != nil {
		return false, err
	}

	return resp.ResponseFlag == 1, nil
}

// ClimateOnRequest sends a request to turn on the climate control
// system.  This is an asynchronous operation: it returns a "result
// key" that can be used to poll for status with the
// CheckClimateOnRequest method.
func (s *Session) ClimateOnRequest() (string, error) {
	var resp struct {
		baseResponse
		ResultKey string `json:"resultKey"`
	}

	if err := s.apiRequest("ACRemoteRequest.php", nil, &resp); err != nil {
		return "", err
	}

	return resp.ResultKey, nil
}

// CheckClimateOnRequest returns whether the ClimateOnRequest has
// finished.
func (s *Session) CheckClimateOnRequest(resultKey string) (bool, error) {
	var resp struct {
		baseResponse
		ResponseFlag    int    `json:"responseFlag,string"` // 0 or 1
		OperationResult string `json:"operationResult"`
		ACContinueTime  string `json:"acContinueTime"`
		TimeStamp       cwTime `json:"timeStamp"`
		HVACStatus      string `json:"hvacStatus"`
	}

	params := url.Values{}
	params.Set("resultKey", resultKey)

	if err := s.apiRequest("ACRemoteResult.php", params, &resp); err != nil {
		return false, err
	}

	return resp.ResponseFlag == 1, nil
}

// ChargingRequest begins charging a plugged-in vehicle.
func (s *Session) ChargingRequest() error {
	var resp struct {
		baseResponse
	}

	params := url.Values{}
	params.Set("ExecuteTime", time.Now().In(s.loc).Format("2006-01-02"))

	if err := s.apiRequest("BatteryRemoteChargingRequest.php", params, &resp); err != nil {
		return err
	}

	return nil
}

// CabinTempRequest sends a request to get the cabin temperature. This is an
// asynchronous operation: it returns a "result key" that can be used
// to poll for status with the CheckCabinTempRequest method.
func (s *Session) CabinTempRequest() (string, error) {
	var resp struct {
		baseResponse
		ResultKey string `json:"resultKey"`
	}

	if err := s.apiRequest("GetInteriorTemperatureRequestForNsp.php", nil, &resp); err != nil {
		return "", err
	}
	return resp.ResultKey, nil
}

// CheckCabinTempRequest returns whether the CabinTempRequest has finished.
func (s *Session) CheckCabinTempRequest(resultKey string) (bool, error) {
	var resp struct {
		baseResponse
		ResponseFlag int `json:"responseFlag,string"` // 0 or 1
		Temperature  int `json:"Inc_temp"`
	}

	params := url.Values{}
	params.Set("resultKey", resultKey)

	if err := s.apiRequest("GetInteriorTemperatureResultForNsp.php", params, &resp); err != nil {
		return false, err
	}
	s.cabinTemp = resp.Temperature

	return resp.ResponseFlag == 1, nil
}

// GetCabinTemp returns the latest cached cabin temperature result.
func (s *Session) GetCabinTemp() int {
	return s.cabinTemp
}

// TripDetail holds the details of each trip.  All of the parsed detail is
// used in both the response and the MonthlyStatistics.
type TripDetail struct {
	//              "PriceSimulatorDetailInfoTrip": [
	//                {
	//                  "TripId": "1",
	//                  "PowerConsumptTotal": "2461.12",
	//                  "PowerConsumptMoter": "3812.22",
	//                  "PowerConsumptMinus": "1351.1",
	//                  "TravelDistance": "17841",
	//                  "ElectricMileage": "13.8",
	//                  "CO2Reduction": "3",
	//                  "MapDisplayFlg": "NONACTIVE",
	//                  "GpsDatetime": "2018-08-05T10:18:47"
	//                },
	TripId             int       `json:",string"`
	PowerConsumedTotal float64   `json:"PowerConsumptTotal,string"`
	PowerConsumedMotor float64   `json:"PowerConsumptMoter,string"`
	PowerRegenerated   float64   `json:"PowerConsumptMinus,string"`
	Meters             int       `json:"TravelDistance,string"`
	Efficiency         float64   `json:"ElectricMileage,string"`
	CO2Reduction       int       `json:",string"`
	MapDisplayFlag     string    `json:"MapDisplayFlg"`
	GPSDateTime        cwTime    `json:"GpsDatetime"`
	Started            time.Time `json:",omitempty"`
}

// DateDetail is the detail for a single date
type DateDetail struct {
	TargetDate string
	Trips      []TripDetail
}

// MonthlyTotals holds the various totals of things for the whole month
type MonthlyTotals struct {
	Trips              int     `json:"TotalNumberOfTrips,string"`
	PowerConsumed      float64 `json:"TotalPowerConsumptTotal,string"`
	PowerConsumedMotor float64 `json:"TotalPowerConsumptMoter,string"`
	PowerRegenerated   float64 `json:"TotalPowerConsumptMinus,string"`
	MetersTravelled    int     `json:"TotalTravelDistance,string"`
	Efficiency         float64 `json:"TotalElectricMileage,string"`
	CO2Reduction       int     `json:"TotalCO2Reductiont,string"`
}

// MonthlyStatistics is the structure returned which includes
// all of the trips and all of the totals as well as the electricity rate
// informtion that has been supplied to CarWings.
type MonthlyStatistics struct {
	EfficiencyScale string
	ElectricityRate float64
	ElectricityBill float64
	Dates           []DateDetail
	Total           MonthlyTotals
}

// GetMonthlyStatistics gets the statistics for a particular month
func (s *Session) GetMonthlyStatistics(month time.Time) (MonthlyStatistics, error) {
	//  {
	//    "status": 200,
	//    "PriceSimulatorDetailInfoResponsePersonalData": {
	//      "TargetMonth": "201808",
	//      "TotalPowerConsumptTotal": "55.88882",
	//      "TotalPowerConsumptMoter": "71.44184",
	//      "TotalPowerConsumptMinus": "15.55302",
	//      "ElectricPrice": "0.15",
	//      "ElectricBill": "8.3833230",
	//      "ElectricCostScale": "kWh/100km",
	//      "MainRateFlg": "COUNTRY",
	//      "ExistFlg": "EXIST",
	//      "PriceSimulatorDetailInfoDateList": {
	//        "PriceSimulatorDetailInfoDate": [
	//          {
	//            "TargetDate": "2018-08-05",
	//            "PriceSimulatorDetailInfoTripList": {
	//              "PriceSimulatorDetailInfoTrip": [
	//                {
	//                  "TripId": "1",
	//                  "PowerConsumptTotal": "2461.12",
	//                  "PowerConsumptMoter": "3812.22",
	//                  "PowerConsumptMinus": "1351.1",
	//                  "TravelDistance": "17841",
	//                  "ElectricMileage": "13.8",
	//                  "CO2Reduction": "3",
	//                  "MapDisplayFlg": "NONACTIVE",
	//                  "GpsDatetime": "2018-08-05T10:18:47"
	//                },
	//                { ... repeats for each trip ... }
	//              ]
	//            },
	//            "DisplayDate": "Aug 05"
	//          },
	//          { ... repeats for each day ... }
	//        ]
	//      },
	//      "PriceSimulatorTotalInfo": {
	//        "TotalNumberOfTrips": "23",
	//        "TotalPowerConsumptTotal": "55.88882",
	//        "TotalPowerConsumptMoter": "71.44184",
	//        "TotalPowerConsumptMinus": "15.55302",
	//        "TotalTravelDistance": "416252",
	//        "TotalElectricMileage": "0.0134",
	//        "TotalCO2Reductiont": "72"
	//      },
	//      "DisplayMonth": "Aug/2018"
	//    }
	//  }
	type detailInfoDate struct {
		//      "PriceSimulatorDetailInfoDateList": {
		//        "PriceSimulatorDetailInfoDate": [
		//          {
		//            "TargetDate": "2018-08-05",
		//            "PriceSimulatorDetailInfoTripList": {
		//              "PriceSimulatorDetailInfoTrip": [
		TargetDate string
		// DisplayDate string  // ignored
		Trips struct {
			List []TripDetail `json:"PriceSimulatorDetailInfoTrip"`
		} `json:"PriceSimulatorDetailInfoTripList"`
	}

	var resp struct {
		baseResponse
		Data struct {
			TargetMonth string
			// The following three fields are ignored because they also appear in the totals
			// - TotalPowerConsumptTotal
			// - TotalPowerConsumptMoter
			// - TotalPowerConsumptMinus
			ElectricPrice     float64 `json:",string"`
			ElectricBill      float64 `json:",string"`
			ElectricCostScale string
			// The following two fields are ignored because their meaning is unclear
			// - MainRateFlg
			// - ExistFlg
			Detail struct {
				RawList json.RawMessage  `json:"PriceSimulatorDetailInfoDate"`
				List    []detailInfoDate `json:"-"`
			} `json:"PriceSimulatorDetailInfoDateList"`
			Total MonthlyTotals `json:"PriceSimulatorTotalInfo"`
		} `json:"PriceSimulatorDetailInfoResponsePersonalData"`
		// DisplayMonth string  // ignored
	}

	ms := MonthlyStatistics{}
	params := url.Values{}
	params.Set("TargetMonth", month.In(s.loc).Format("200601"))

	if err := s.apiRequest("PriceSimulatorDetailInfoRequest.php", params, &resp); err != nil {
		return ms, err
	}

	// This field is an empty string instead of an object if there's no data.
	if string(resp.Data.Detail.RawList) != `""` {
		err := json.Unmarshal(resp.Data.Detail.RawList, &resp.Data.Detail.List)
		if err != nil {
			return ms, err
		}
	}

	ms.EfficiencyScale = resp.Data.ElectricCostScale
	ms.ElectricityRate = resp.Data.ElectricPrice
	ms.ElectricityBill = resp.Data.ElectricBill
	ms.Total = resp.Data.Total
	ms.Dates = make([]DateDetail, 0, 31)
	for i := 0; i < len(resp.Data.Detail.List); i++ {
		trips := make([]TripDetail, 0, 10)
		for j := 0; j < len(resp.Data.Detail.List[i].Trips.List); j++ {
			trip := resp.Data.Detail.List[i].Trips.List[j]
			trip.Started = time.Time(trip.GPSDateTime)
			trips = append(trips, trip)
		}
		ms.Dates = append(ms.Dates, DateDetail{
			TargetDate: resp.Data.Detail.List[i].TargetDate,
			Trips:      trips,
		})
	}

	return ms, nil
}

// DailyStatistics holds the statistics for a day
type DailyStatistics struct {
	TargetDate              time.Time
	EfficiencyScale         string
	Efficiency              float64 `json:",string"`
	EfficiencyLevel         int     `json:",string"`
	PowerConsumedMotor      float64 `json:",string"`
	PowerConsumedMotorLevel int     `json:",string"`
	PowerRegeneration       float64 `json:",string"`
	PowerRegenerationLevel  int     `json:",string"`
	PowerConsumedAUX        float64 `json:",string"`
	PowerConsumedAUXLevel   int     `json:",string"`
}

// GetDailyStatistics returns the statistics for a specified Date^W^W^Wtoday
func (s *Session) GetDailyStatistics(day time.Time) (DailyStatistics, error) {
	//  {
	//    "status": 200,
	//    "DriveAnalysisBasicScreenResponsePersonalData": {
	//      "DateSummary": {
	//        "TargetDate": "2018-08-12",
	//        "ElectricMileage": "11.9",
	//        "ElectricMileageLevel": "5",
	//        "PowerConsumptMoter": "140.5",
	//        "PowerConsumptMoterLevel": "5",
	//        "PowerConsumptMinus": "29.3",
	//        "PowerConsumptMinusLevel": "2",
	//        "PowerConsumptAUX": "7.4",
	//        "PowerConsumptAUXLevel": "5",
	//        "DisplayDate": "Aug 12, 18"
	//      },
	//      "ElectricCostScale": "kWh/100km"
	//    },
	//    "AdviceList": {
	//      "Advice": {
	//        "title": "Drive Tip:",
	//        "body": "Use remote climate control or timer so that the cabin will be at a comfortable temperature before starting.  This allows the car to save energy whilst being driven."
	//      }
	//    }
	//  }

	var resp struct {
		baseResponse
		Data struct {
			Stats struct {
				TargetDate              string
				ElectricMileage         float64 `json:",string"`
				ElectricMileageLevel    int     `json:",string"`
				PowerConsumptMoter      float64 `json:",string"`
				PowerConsumptMoterLevel int     `json:",string"`
				PowerConsumptMinus      float64 `json:",string"`
				PowerConsumptMinusLevel int     `json:",string"`
				PowerConsumptAUX        float64 `json:",string"`
				PowerConsumptAUXLevel   int     `json:",string"`
			} `json:"DateSummary"`
			ElectricCostScale string
		} `json:"DriveAnalysisBasicScreenResponsePersonalData"`
	}

	ds := DailyStatistics{}

	params := url.Values{}
	// TODO: There's a bug getting stats for any day other than today: we have guessed the
	// TODO: name of the `TargetDate` parameter wrong :-(
	// TODO: It isn't `TargetDate` or `DetailTargetDate`
	// On the other hand, we can get/calculate all of this (and more) from the daily records in the
	// MonthlyStatistics response, so maybe it's silly to do it this way?
	// params.Set("DetailTargetDate", day.In(s.loc).Format("2006-01-02"))

	if err := s.apiRequest("DriveAnalysisBasicScreenRequestEx.php", params, &resp); err != nil {
		return ds, err
	}

	if resp.Data.Stats.TargetDate == "" {
		return ds, errors.New("daily driving statistics not available")
	}

	ds.TargetDate, _ = time.ParseInLocation("2006-01-02", resp.Data.Stats.TargetDate, s.loc)
	ds.EfficiencyScale = resp.Data.ElectricCostScale
	ds.Efficiency = resp.Data.Stats.ElectricMileage
	ds.EfficiencyLevel = resp.Data.Stats.ElectricMileageLevel
	ds.PowerConsumedMotor = resp.Data.Stats.PowerConsumptMoter
	ds.PowerConsumedMotorLevel = resp.Data.Stats.PowerConsumptMoterLevel
	ds.PowerRegeneration = resp.Data.Stats.PowerConsumptMinus
	ds.PowerRegenerationLevel = resp.Data.Stats.PowerConsumptMinusLevel
	ds.PowerConsumedAUX = resp.Data.Stats.PowerConsumptAUX
	ds.PowerConsumedAUXLevel = resp.Data.Stats.PowerConsumptAUXLevel

	return ds, nil
}
