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
	"time"

	"golang.org/x/crypto/blowfish"
)

const (
	baseURL = "https://gdcportalgw.its-mo.com/api_v180117_NE/gdc/"

	// Result of the call to InitialApp.php, which appears to
	// always be the same.  It'll probably break at some point but
	// for now... skip it.
	blowfishKey = "uyI5Dj9g8VCOFDnBRUbr3g"

	// Extracted from the NissanConnect EV app
	initialAppStrings = "geORNtsZe5I4lRGjG9GZiA"
)

var (
	// ErrNotLoggedIn is returned whenever an operation is run and
	// the user has not let logged in.
	ErrNotLoggedIn = errors.New("not logged in")

	// ErrUpdateFailed indicates an error talking to the Carwings
	// service when fetching updated vehicle data.
	ErrUpdateFailed = errors.New("failed to retrieve updated info from vehicle")

	// Debug indiciates whether to log HTTP responses to stderr
	Debug = false
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

type Config struct {
	Username    string
	Password    string
	Region      string
	SiUnits     bool
	SessionFile string
}

// Session defines a one or more connections to the Carwings service
type Session struct {
	config          Config
	encpw           string
	VIN             string
	customSessionID string
	tz              string
	loc             *time.Location
	SiUnits         bool
}

// ClimateStatus contains information about the vehicle's climate
// control (AC or heater) status.
type ClimateStatus struct {
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

	// Carwings uses three different date formats 🙄🙄🙄
	t, err := time.Parse(`"2006\/01\/02 15:04"`, string(data))
	if err != nil {
		t, err = time.Parse(`"2006-01-02 15:04:05"`, string(data))
		if err != nil {
			// Also e.g. "UserVehicleBoundTime": "2018-08-04T15:08:33Z"
			t, err = time.Parse(`"2006-01-02T15:04:05Z"`, string(data))
			if err != nil {
				return fmt.Errorf("cannot parse %q as carwings time", string(data))
			}
		}
	}

	*cwt = cwTime(t)
	return nil
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
	if Debug {
		fmt.Fprintf(os.Stderr, "POST %s %s\n", baseURL+endpoint, params)
	}

	resp, err := http.PostForm(baseURL+endpoint, params)
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

	case http.StatusUnauthorized:
		return ErrNotLoggedIn

	default:
		if e := target.ErrorMessage(); e != "" {
			return fmt.Errorf("received status code %d (%s)", s, e)
		}
		return fmt.Errorf("received status code %d", s)
	}
}

// Connect loads an existing saved session, or initialises a new one
func Connect(cfg Config) (*Session, error) {
	var err error

	s := &Session{
		config:  cfg,
		SiUnits: cfg.SiUnits,
	}

	if cfg.SessionFile != "" {
		err = s.Load(cfg.SessionFile)
		if err == nil && s.customSessionID != "" && s.VIN != "" {
			return s, nil
		} else {
			fmt.Fprintln(os.Stderr, "ERROR: ", err.Error())
		}
	}

	return s, s.connect()
}

type sessionData struct {
	CustomSessionID string
	VIN             string
	RegionCode      string
	TimeZone        string
	SiUnits         bool
}

// connect may be called after a 401 response to reconnect the session
//
// If connect returns without error then it should be OK to retry the
// request which previously returned the 401 response
func (s *Session) connect() error {
	if s.config.SessionFile == "" {
		return nil
	}

	params := url.Values{}
	params.Set("initial_app_strings", initialAppStrings)

	var initResp struct {
		baseResponse
		Message string `json:"message"`
		Baseprm string `json:"baseprm"`
	}

	err := apiRequest("InitialApp.php", params, &initResp)
	if err != nil {
		return err
	}

	s.encpw, err = encrypt(s.config.Password, initResp.Baseprm)
	if err != nil {
		return err
	}

	err = s.Login()
	if err != nil {
		return err
	}

	if s.config.SessionFile != "" && s.customSessionID != "" && s.VIN != "" {
		err = s.Save(s.config.SessionFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: ", err.Error())
		}
	}
	return nil
}

func (s Session) Save(fileName string) error {
	if fileName == "" {
		return nil
	}
	if fileName[0] == '~' {
		fileName = os.Getenv("HOME") + fileName[1:]
	}

	f, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	sd := sessionData{
		CustomSessionID: s.customSessionID,
		VIN:             s.VIN,
		TimeZone:        s.tz,
		SiUnits:         s.SiUnits,
	}

	data, _ := json.Marshal(sd)
	_, err = f.Write(data)
	if err != nil {
		return err
	}
	return f.Close()
}

func (s *Session) Load(fileName string) error {
	if fileName == "" {
		return nil
	}
	if fileName[0] == '~' {
		fileName = os.Getenv("HOME") + fileName[1:]
	}

	f, err := os.Open(fileName)
	if err != nil {
		return err
	}

	sd := sessionData{}
	decoder := json.NewDecoder(f)
	err = decoder.Decode(&sd)
	if err != nil {
		return err
	}
	f.Close()

	s.customSessionID = sd.CustomSessionID
	s.VIN = sd.VIN
	s.tz = sd.TimeZone
	s.SiUnits = sd.SiUnits

	s.loc, err = time.LoadLocation(s.tz)
	if err != nil {
		s.loc = time.Local
		s.tz = time.Local.String()
	}

	return nil
}

func (s *Session) Login() error {
	params := url.Values{}
	params.Set("initial_app_strings", initialAppStrings)

	params.Set("UserId", s.config.Username)
	params.Set("Password", s.encpw)
	params.Set("RegionCode", s.config.Region)

	// Not a comprehensive representation, just what we need
	var loginResp struct {
		baseResponse

		VehicleInfo []struct {
			VIN               string `json:"vin"`
			Nickname          string `json:"nickname,omitempty"`
			Charger20066      string `json:"charger20066,omitempty"`
			TelematicsEnabled string `json:"telematicsEnabled,omitempty"`
			CustomSessionID   string `json:"custom_sessionid"`
		} `json:"vehicleInfo"`

		CustomerInfo struct {
			Timezone                    string
			Language                    string
			OwnerId                     string
			EMailAddress                string
			Nickname                    string
			Country                     string
			VehicleImage                string
			UserVehicleBoundDurationSec string
			VehicleInfo                 struct {
				VIN                  string
				DCMID                string
				SIMID                string
				NAVIID               string
				EncryptedNAVIID      string
				MSN                  string
				LastVehicleLoginTime string
				UserVehicleBoundTime string
				LastDCMUseTime       string
				NonaviFlg            string
				CarName              string
				CarImage             string
			}
		}
	}
	var err error
	if err = apiRequest("UserLoginRequest.php", params, &loginResp); err != nil {
		return err
	}

	vi := loginResp.VehicleInfo[0]

	s.customSessionID = vi.CustomSessionID
	s.VIN = vi.VIN
	s.tz = loginResp.CustomerInfo.Timezone

	s.loc, err = time.LoadLocation(s.tz)
	if err != nil {
		s.loc = time.Local
		s.tz = time.Local.String()
	}

	return nil
}

func (s *Session) apiRequest(endpoint string, params url.Values, target response) error {
	params = s.setCommonParams(params)

	err := apiRequest(endpoint, params, target)
	if err == ErrNotLoggedIn {
		if err := s.connect(); err != nil {
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

	params.Set("RegionCode", s.config.Region)
	params.Set("VIN", s.VIN)
	params.Set("custom_sessionid", s.customSessionID)
	params.Set("tz", s.tz)
	return params
}

// UpdateStatus asks the Carwings service to request an update from
// the vehicle.  This is an asynchronous operation: it returns a
// "result key" that can be used to poll for status with the
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
	var resp struct {
		baseResponse
		BatteryStatusRecords struct {
			BatteryStatus struct {
				BatteryChargingStatus     string
				BatteryCapacity           int `json:",string"`
				BatteryRemainingAmount    int `json:",string"`
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
	}
	if err := s.apiRequest("BatteryStatusRecordsRequest.php", nil, &resp); err != nil {
		return BatteryStatus{}, err
	}

	batrec := resp.BatteryStatusRecords
	acOn, _ := batrec.CruisingRangeAcOn.Float64()
	acOff, _ := batrec.CruisingRangeAcOff.Float64()

	soc := batrec.BatteryStatus.SOC.Value
	if soc == 0 {
		soc = int(math.Round(float64(batrec.BatteryStatus.BatteryRemainingAmount) / float64(batrec.BatteryStatus.BatteryCapacity)))
	}

	bs := BatteryStatus{
		Timestamp:          time.Time(batrec.NotificationDateAndTime).In(s.loc),
		Capacity:           batrec.BatteryStatus.BatteryCapacity,
		Remaining:          batrec.BatteryStatus.BatteryRemainingAmount,
		StateOfCharge:      batrec.BatteryStatus.SOC.Value,
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
	if bs.StateOfCharge == 0 && bs.Remaining != 0 && bs.Capacity != 0 {
		bs.StateOfCharge = (bs.Remaining * 100) / bs.Capacity
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
		return ClimateStatus{}, errors.New("climate status not available")
	}

	var racr remoteACRecords
	if err := json.Unmarshal(resp.RemoteACRecords, &racr); err != nil {
		return ClimateStatus{}, err
	}

	cs := ClimateStatus{
		Running:         racr.RemoteACOperation == "START",
		PluginState:     PluginState(racr.PluginState),
		BatteryDuration: racr.ACDurationBatterySec,
		PluggedDuration: racr.ACDurationPluggedSec,
		TemperatureUnit: racr.PreAC_unit,
		Temperature:     racr.PreAC_temp,
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

// LocateRequest sends a request to locate the vehicle.  This is an
// asynchronous operation: it returns a "result key" that can be used
// to poll for status with the CheckLocateRequest method.
func (s *Session) LocateRequest() (string, error) {
	var resp struct {
		baseResponse
		ResultKey string `json:"resultKey"`
	}

	if err := s.apiRequest("MyCarFinderRequest.php", nil, &resp); err != nil {
		return "", err
	}

	return resp.ResultKey, nil
}

// CheckLocateRequest returns whether the LocateRequest has finished.
func (s *Session) CheckLocateRequest(resultKey string) (bool, error) {
	var resp struct {
		baseResponse
		ResponseFlag int `json:"responseFlag,string"` // 0 or 1
	}

	params := url.Values{}
	params.Set("resultKey", resultKey)

	if err := s.apiRequest("MyCarFinderResultRequest.php", params, &resp); err != nil {
		return false, err
	}

	return resp.ResponseFlag == 1, nil
}

// LocateVehicle requests the last-known location of the vehicle from
// the Carwings service.  This data is not real-time.  A timestamp of
// the most recent update is available in the returned VehicleLocation
// value.
func (s *Session) LocateVehicle() (VehicleLocation, error) {
	var resp struct {
		baseResponse
		ReceivedDate cwTime `json:"receivedDate"`
		TargetDate   cwTime
		Lat          string
		Lng          string
	}

	if err := s.apiRequest("MyCarFinderLatLng.php", nil, &resp); err != nil {
		return VehicleLocation{}, err
	}

	if time.Time(resp.ReceivedDate).IsZero() {
		return VehicleLocation{}, errors.New("no location data available")
	}

	return VehicleLocation{
		Timestamp: time.Time(resp.ReceivedDate).In(s.loc),
		Latitude:  resp.Lat,
		Longitude: resp.Lng,
	}, nil
}
