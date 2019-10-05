package main

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/sqlx"
	goji "goji.io"
	"goji.io/pat"
	"golang.org/x/crypto/pbkdf2"
	// "sync"
)

var (
	banner        = `ISUTRAIN API`
	TrainClassMap = map[string]string{"express": "最速", "semi_express": "中間", "local": "遅いやつ"}
)

var dbx *sqlx.DB

// DB定義

type Station struct {
	ID                int     `json:"id" db:"id"`
	Name              string  `json:"name" db:"name"`
	Distance          float64 `json:"-" db:"distance"`
	IsStopExpress     bool    `json:"is_stop_express" db:"is_stop_express"`
	IsStopSemiExpress bool    `json:"is_stop_semi_express" db:"is_stop_semi_express"`
	IsStopLocal       bool    `json:"is_stop_local" db:"is_stop_local"`
}

type DistanceFare struct {
	Distance float64 `json:"distance" db:"distance"`
	Fare     int     `json:"fare" db:"fare"`
}

type Fare struct {
	TrainClass     string    `json:"train_class" db:"train_class"`
	SeatClass      string    `json:"seat_class" db:"seat_class"`
	StartDate      time.Time `json:"start_date" db:"start_date"`
	FareMultiplier float64   `json:"fare_multiplier" db:"fare_multiplier"`
}

type Train struct {
	Date         time.Time `json:"date" db:"date"`
	DepartureAt  string    `json:"departure_at" db:"departure_at"`
	TrainClass   string    `json:"train_class" db:"train_class"`
	TrainName    string    `json:"train_name" db:"train_name"`
	StartStation string    `json:"start_station" db:"start_station"`
	LastStation  string    `json:"last_station" db:"last_station"`
	IsNobori     bool      `json:"is_nobori" db:"is_nobori"`
	TrainClassID int
}

type Seat struct {
	TrainClass    string `json:"train_class" db:"train_class"`
	CarNumber     int    `json:"car_number" db:"car_number"`
	SeatColumn    string `json:"seat_column" db:"seat_column"`
	SeatRow       int    `json:"seat_row" db:"seat_row"`
	SeatClass     string `json:"seat_class" db:"seat_class"`
	IsSmokingSeat bool   `json:"is_smoking_seat" db:"is_smoking_seat"`
}

type Reservation struct {
	ReservationId int        `json:"reservation_id" db:"reservation_id"`
	UserId        *int       `json:"user_id" db:"user_id"`
	Date          *time.Time `json:"date" db:"date"`
	TrainClass    string     `json:"train_class" db:"train_class"`
	TrainName     string     `json:"train_name" db:"train_name"`
	DepartureID   int        `json:"-" db:"departure"`
	Departure     string     `json:"departure" db:"-"`
	ArrivalID     int        `json:"-" db:"arrival"`
	Arrival       string     `json:"arrival" db:"-"`
	PaymentStatus string     `json:"payment_status" db:"payment_status"`
	Status        string     `json:"status" db:"status"`
	PaymentId     string     `json:"payment_id,omitempty" db:"payment_id"`
	Adult         int        `json:"adult" db:"adult"`
	Child         int        `json:"child" db:"child"`
	Amount        int        `json:"amount" db:"amount"`
}

func (r *Reservation) fillStationByID() {
	if r.Departure == "" && r.DepartureID > 0 {
		r.Departure = stationMasterByID[r.DepartureID].Name
		r.Arrival = stationMasterByID[r.ArrivalID].Name
	}
}

type SeatReservation struct {
	ReservationId int    `json:"reservation_id,omitempty" db:"reservation_id"`
	CarNumber     int    `json:"car_number,omitempty" db:"car_number"`
	SeatRow       int    `json:"seat_row" db:"seat_row"`
	SeatColumn    string `json:"seat_column" db:"seat_column"`
}

// 未整理

type CarInformation struct {
	Date                string                 `json:"date"`
	TrainClass          string                 `json:"train_class"`
	TrainName           string                 `json:"train_name"`
	CarNumber           int                    `json:"car_number"`
	SeatInformationList []SeatInformation      `json:"seats"`
	Cars                []SimpleCarInformation `json:"cars"`
}

type SimpleCarInformation struct {
	CarNumber int    `json:"car_number"`
	SeatClass string `json:"seat_class"`
}

type SeatInformation struct {
	Row           int    `json:"row"`
	Column        string `json:"column"`
	Class         string `json:"class"`
	IsSmokingSeat bool   `json:"is_smoking_seat"`
	IsOccupied    bool   `json:"is_occupied"`
}

type SeatInformationByCarNumber struct {
	CarNumber           int               `json:"car_number"`
	SeatInformationList []SeatInformation `json:"seats"`
}

type TrainSearchResponse struct {
	Class            string            `json:"train_class"`
	Name             string            `json:"train_name"`
	Start            string            `json:"start"`
	Last             string            `json:"last"`
	Departure        string            `json:"departure"`
	Arrival          string            `json:"arrival"`
	DepartureTime    string            `json:"departure_time"`
	ArrivalTime      string            `json:"arrival_time"`
	SeatAvailability map[string]string `json:"seat_availability"`
	Fare             map[string]int    `json:"seat_fare"`
}

type User struct {
	ID             int64
	Email          string `json:"email"`
	Password       string `json:"password"`
	Salt           []byte `db:"salt"`
	HashedPassword []byte `db:"super_secure_password"`
}

type TrainReservationRequest struct {
	Date          string        `json:"date"`
	TrainName     string        `json:"train_name"`
	TrainClass    string        `json:"train_class"`
	CarNumber     int           `json:"car_number"`
	IsSmokingSeat bool          `json:"is_smoking_seat"`
	SeatClass     string        `json:"seat_class"`
	Departure     string        `json:"departure"`
	Arrival       string        `json:"arrival"`
	Child         int           `json:"child"`
	Adult         int           `json:"adult"`
	Column        string        `json:"Column"`
	Seats         []RequestSeat `json:"seats"`
}

type RequestSeat struct {
	Row    int    `json:"row"`
	Column string `json:"column"`
}

type TrainReservationResponse struct {
	ReservationId int64 `json:"reservation_id"`
	Amount        int   `json:"amount"`
	IsOk          bool  `json:"is_ok"`
}

type ReservationPaymentRequest struct {
	CardToken     string `json:"card_token"`
	ReservationId int    `json:"reservation_id"`
}

type ReservationPaymentResponse struct {
	IsOk bool `json:"is_ok"`
}

type PaymentInformationRequest struct {
	CardToken     string `json:"card_token"`
	ReservationId int    `json:"reservation_id"`
	Amount        int    `json:"amount"`
}

type PaymentInformation struct {
	PayInfo PaymentInformationRequest `json:"payment_information"`
}

type PaymentResponse struct {
	PaymentId string `json:"payment_id"`
	IsOk      bool   `json:"is_ok"`
}

type ReservationResponse struct {
	ReservationId int               `json:"reservation_id"`
	Date          string            `json:"date"`
	TrainClass    string            `json:"train_class"`
	TrainName     string            `json:"train_name"`
	CarNumber     int               `json:"car_number"`
	SeatClass     string            `json:"seat_class"`
	Amount        int               `json:"amount"`
	Adult         int               `json:"adult"`
	Child         int               `json:"child"`
	Departure     string            `json:"departure"`
	Arrival       string            `json:"arrival"`
	DepartureTime string            `json:"departure_time"`
	ArrivalTime   string            `json:"arrival_time"`
	Seats         []SeatReservation `json:"seats"`
}

type CancelPaymentInformationRequest struct {
	PaymentId string `json:"payment_id"`
}

type CancelPaymentInformationResponse struct {
	IsOk bool `json:"is_ok"`
}

type Settings struct {
	PaymentAPI string `json:"payment_api"`
}

type InitializeResponse struct {
	AvailableDays int    `json:"available_days"`
	Language      string `json:"language"`
}

type AuthResponse struct {
	Email string `json:"email"`
}

const (
	sessionName   = "session_isutrain"
	availableDays = 10
)

var (
	store sessions.Store = sessions.NewCookieStore([]byte(secureRandomStr(20)))
)

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, World")
}

func messageResponse(w http.ResponseWriter, message string) {
	e := map[string]interface{}{
		"is_error": false,
		"message":  message,
	}
	errResp, _ := json.Marshal(e)
	w.Write(errResp)
}

func errorResponse(w http.ResponseWriter, errCode int, message string) {
	e := map[string]interface{}{
		"is_error": true,
		"message":  message,
	}
	errResp, _ := json.Marshal(e)

	w.WriteHeader(errCode)
	w.Write(errResp)
}

func getSession(r *http.Request) *sessions.Session {
	session, _ := store.Get(r, sessionName)

	return session
}

func getUser(r *http.Request) (user User, errCode int, errMsg string) {
	session := getSession(r)
	userID, ok := session.Values["user_id"]
	if !ok {
		return user, http.StatusUnauthorized, "no session"
	}

	err := dbx.GetContext(r.Context(), &user, "SELECT * FROM `users` WHERE `id` = ?", userID)
	if err == sql.ErrNoRows {
		return user, http.StatusUnauthorized, "user not found"
	}
	if err != nil {
		log.Print(err)
		return user, http.StatusInternalServerError, "db error"
	}

	return user, http.StatusOK, ""
}

func secureRandomStr(b int) string {
	k := make([]byte, b)
	if _, err := crand.Read(k); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", k)
}

var (
	distanceFareMaster = []DistanceFare{
		{0, 2500},
		{50, 3000},
		{75, 3700},
		{100, 4500},
		{150, 5200},
		{200, 6000},
		{300, 7200},
		{400, 8300},
		{500, 12000},
		{1000, 20000},
	}

	mReservationCache sync.Mutex
	reservationCache  map[int]Reservation
)

func initReservationCache() {
	mReservationCache.Lock()
	defer mReservationCache.Unlock()

	reservationCache = make(map[int]Reservation)
}

func getReservation(id int) (Reservation, bool) {
	mReservationCache.Lock()
	defer mReservationCache.Unlock()

	r, ok := reservationCache[id]
	return r, ok
}

func insertReservation(r Reservation) {
	mReservationCache.Lock()
	defer mReservationCache.Unlock()

	reservationCache[r.ReservationId] = r
}

func deleteReservation(id int) {
	mReservationCache.Lock()
	defer mReservationCache.Unlock()

	delete(reservationCache, id)
}

func distanceFareHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(distanceFareMaster)
}

func getDistanceFare(ctx context.Context, origToDestDistance float64) (int, error) {
	lastDistance := 0.0
	lastFare := 0
	for _, distanceFare := range distanceFareMaster {
		//log.Println(origToDestDistance, distanceFare.Distance, distanceFare.Fare)
		if float64(lastDistance) < origToDestDistance && origToDestDistance < float64(distanceFare.Distance) {
			break
		}
		lastDistance = distanceFare.Distance
		lastFare = distanceFare.Fare
	}

	return lastFare, nil
}

func fareCalc(ctx context.Context, date time.Time, depStation int, destStation int, trainClass, seatClass string) (int, error) {
	//
	// 料金計算メモ
	// 距離運賃(円) * 期間倍率(繁忙期なら2倍等) * 車両クラス倍率(急行・各停等) * 座席クラス倍率(プレミアム・指定席・自由席)
	//
	var err error
	var fromStation, toStation Station
	var ok bool

	if fromStation, ok = stationMasterByID[depStation]; !ok {
		err = fmt.Errorf("depStation(%v) not found", depStation)
	}
	if toStation, ok = stationMasterByID[destStation]; !ok {
		err = fmt.Errorf("destStation(%v) not found", depStation)
	}

	if err != nil {
		log.Print(err)
		return 0, err
	}

	distFare, err := getDistanceFare(ctx, math.Abs(toStation.Distance-fromStation.Distance))
	if err != nil {
		return 0, err
	}

	// 期間・車両・座席クラス倍率
	fareList := []Fare{}
	query := "SELECT * FROM fare_master WHERE train_class=? AND seat_class=? ORDER BY start_date"
	err = dbx.SelectContext(ctx, &fareList, query, trainClass, seatClass)
	if err != nil {
		return 0, err
	}

	if len(fareList) == 0 {
		return 0, fmt.Errorf("fare_master does not exists")
	}

	selectedFare := fareList[0]
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	for _, fare := range fareList {
		if !date.Before(fare.StartDate) {
			selectedFare = fare
		}
	}

	return int(float64(distFare) * selectedFare.FareMultiplier), nil
}

func getStationsHandler(w http.ResponseWriter, r *http.Request) {
	/*
		駅一覧
			GET /api/stations

		return []Station{}
	*/

	stations := stationMaster

	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(stations)
}

func trainSearchHandler(w http.ResponseWriter, r *http.Request) {
	/*
		列車検索
			GET /train/search?use_at=<ISO8601形式の時刻> & from=東京 & to=大阪

		return
			料金
			空席情報
			発駅と着駅の到着時刻

	*/

	jst := time.FixedZone("JST", 9*60*60)
	date, err := time.Parse(time.RFC3339, r.URL.Query().Get("use_at"))
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	date = date.In(jst)

	if !checkAvailableDate(date) {
		errorResponse(w, http.StatusNotFound, "予約可能期間外です")
		return
	}

	trainClass := r.URL.Query().Get("train_class")
	fromName := r.URL.Query().Get("from")
	toName := r.URL.Query().Get("to")

	adult, _ := strconv.Atoi(r.URL.Query().Get("adult"))
	child, _ := strconv.Atoi(r.URL.Query().Get("child"))

	var fromStation, toStation Station
	var ok bool
	// From
	if fromStation, ok = stationMasterByName[fromName]; !ok {
		log.Printf("fromStation(%s): no rows", fromName)
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	// To
	if toStation, ok = stationMasterByName[toName]; !ok {
		log.Print("toStation(%s): no rows", toName)
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	isNobori := false
	if fromStation.Distance > toStation.Distance {
		isNobori = true
	}

	var usableTrainClassList []int
	if trainClass == "" {
		usableTrainClassList = getUsableTrainClassIDList(fromStation, toStation)
	} else {
		usableTrainClassList = []int{trainClassID[trainClass]}
	}

	trainList := SelectTrainMaster(date, usableTrainClassList, isNobori)

	stations := stationsOrderByDistance(isNobori)

	log.Println("From", fromStation)
	log.Println("To", toStation)

	trainSearchResponseList := []TrainSearchResponse{}

	for _, train := range trainList {
		isSeekedToFirstStation := false
		isContainsOriginStation := false
		isContainsDestStation := false
		i := 0

		for _, station := range stations {

			if !isSeekedToFirstStation {
				// 駅リストを列車の発駅まで読み飛ばして頭出しをする
				// 列車の発駅以前は止まらないので無視して良い
				if station.Name == train.StartStation {
					isSeekedToFirstStation = true
				} else {
					continue
				}
			}

			if station.ID == fromStation.ID {
				// 発駅を経路中に持つ編成の場合フラグを立てる
				isContainsOriginStation = true
			}
			if station.ID == toStation.ID {
				if isContainsOriginStation {
					// 発駅と着駅を経路中に持つ編成の場合
					isContainsDestStation = true
					break
				} else {
					// 出発駅より先に終点が見つかったとき
					log.Println("なんかおかしい")
					break
				}
			}
			if station.Name == train.LastStation {
				// 駅が見つからないまま当該編成の終点に着いてしまったとき
				break
			}
			i++
		}

		if isContainsOriginStation && isContainsDestStation {
			// 列車情報

			// 所要時間
			var departure, arrival string

			err = dbx.GetContext(r.Context(), &departure, "SELECT departure FROM train_timetable_master WHERE date=? AND train_class=? AND train_name=? AND station=?", date.Format("2006/01/02"), train.TrainClass, train.TrainName, fromStation.Name)
			if err != nil {
				errorResponse(w, http.StatusInternalServerError, err.Error())
				return
			}

			departureDate, err := time.Parse("2006/01/02 15:04:05 -07:00 MST", fmt.Sprintf("%s %s +09:00 JST", date.Format("2006/01/02"), departure))
			if err != nil {
				errorResponse(w, http.StatusInternalServerError, err.Error())
				return
			}

			if !date.Before(departureDate) {
				// 乗りたい時刻より出発時刻が前なので除外
				continue
			}

			err = dbx.GetContext(r.Context(), &arrival, "SELECT arrival FROM train_timetable_master WHERE date=? AND train_class=? AND train_name=? AND station=?", date.Format("2006/01/02"), train.TrainClass, train.TrainName, toStation.Name)
			if err != nil {
				errorResponse(w, http.StatusInternalServerError, err.Error())
				return
			}

			avail_seats, err := train.getAvailableSeats(r.Context(), fromStation, toStation)
			if err != nil {
				errorResponse(w, http.StatusBadRequest, err.Error())
				return
			}
			var premium_avail_seats []Seat
			var premium_smoke_avail_seats []Seat
			var reserved_avail_seats []Seat
			var reserved_smoke_avail_seats []Seat
			for _, s := range avail_seats {
				switch s.SeatClass {
				case "premium":
					if s.IsSmokingSeat {
						premium_smoke_avail_seats = append(premium_smoke_avail_seats, s)
					} else {
						premium_avail_seats = append(premium_avail_seats, s)
					}
				case "reserved":
					if s.IsSmokingSeat {
						reserved_smoke_avail_seats = append(reserved_smoke_avail_seats, s)
					} else {
						reserved_avail_seats = append(reserved_avail_seats, s)
					}
				}
			}

			premium_avail := "○"
			if len(premium_avail_seats) == 0 {
				premium_avail = "×"
			} else if len(premium_avail_seats) < 10 {
				premium_avail = "△"
			}

			premium_smoke_avail := "○"
			if len(premium_smoke_avail_seats) == 0 {
				premium_smoke_avail = "×"
			} else if len(premium_smoke_avail_seats) < 10 {
				premium_smoke_avail = "△"
			}

			reserved_avail := "○"
			if len(reserved_avail_seats) == 0 {
				reserved_avail = "×"
			} else if len(reserved_avail_seats) < 10 {
				reserved_avail = "△"
			}

			reserved_smoke_avail := "○"
			if len(reserved_smoke_avail_seats) == 0 {
				reserved_smoke_avail = "×"
			} else if len(reserved_smoke_avail_seats) < 10 {
				reserved_smoke_avail = "△"
			}

			// 空席情報
			seatAvailability := map[string]string{
				"premium":        premium_avail,
				"premium_smoke":  premium_smoke_avail,
				"reserved":       reserved_avail,
				"reserved_smoke": reserved_smoke_avail,
				"non_reserved":   "○",
			}

			// 料金計算
			premiumFare, err := fareCalc(r.Context(), date, fromStation.ID, toStation.ID, train.TrainClass, "premium")
			if err != nil {
				errorResponse(w, http.StatusBadRequest, err.Error())
				return
			}
			premiumFare = premiumFare*adult + premiumFare/2*child

			reservedFare, err := fareCalc(r.Context(), date, fromStation.ID, toStation.ID, train.TrainClass, "reserved")
			if err != nil {
				errorResponse(w, http.StatusBadRequest, err.Error())
				return
			}
			reservedFare = reservedFare*adult + reservedFare/2*child

			nonReservedFare, err := fareCalc(r.Context(), date, fromStation.ID, toStation.ID, train.TrainClass, "non-reserved")
			if err != nil {
				errorResponse(w, http.StatusBadRequest, err.Error())
				return
			}
			nonReservedFare = nonReservedFare*adult + nonReservedFare/2*child

			fareInformation := map[string]int{
				"premium":        premiumFare,
				"premium_smoke":  premiumFare,
				"reserved":       reservedFare,
				"reserved_smoke": reservedFare,
				"non_reserved":   nonReservedFare,
			}

			trainSearchResponseList = append(trainSearchResponseList, TrainSearchResponse{
				train.TrainClass, train.TrainName, train.StartStation, train.LastStation,
				fromStation.Name, toStation.Name, departure, arrival, seatAvailability, fareInformation,
			})

			if len(trainSearchResponseList) >= 10 {
				break
			}
		}
	}
	resp, err := json.Marshal(trainSearchResponseList)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Write(resp)

}

func trainSeatsHandler(w http.ResponseWriter, r *http.Request) {
	/*
		指定した列車の座席列挙
		GET /train/seats?date=2020-03-01&train_class=のぞみ&train_name=96号&car_number=2&from=大阪&to=東京
	*/

	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	date, err := time.Parse(time.RFC3339, r.URL.Query().Get("date"))
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	date = date.In(jst)

	if !checkAvailableDate(date) {
		errorResponse(w, http.StatusNotFound, "予約可能期間外です")
		return
	}

	trainClass := r.URL.Query().Get("train_class")
	trainName := r.URL.Query().Get("train_name")
	carNumber, _ := strconv.Atoi(r.URL.Query().Get("car_number"))
	fromName := r.URL.Query().Get("from")
	toName := r.URL.Query().Get("to")

	// 対象列車の取得
	train, ok := SelectTrainMasterByName(date, trainClassID[trainClass], trainName)
	if !ok {
		log.Printf("train (%v-%v-%v) not found", date, trainClass, trainName)
		errorResponse(w, http.StatusNotFound, "列車が存在しません")
		return
	}

	var fromStation, toStation Station
	// From
	if fromStation, ok = stationMasterByName[fromName]; !ok {
		log.Printf("fromStation(%s): no rows", fromName)
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	// To
	if toStation, ok = stationMasterByName[toName]; !ok {
		log.Print("toStation(%s): no rows", toName)
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	usableTrainClassList := getUsableTrainClassIDList(fromStation, toStation)
	usable := false
	for _, v := range usableTrainClassList {
		if v == train.TrainClassID {
			usable = true
		}
	}
	if !usable {
		err = fmt.Errorf("invalid train_class")
		log.Print(err)
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	seatList := []Seat{}
	for _, s := range seatMaster[trainClassID[trainClass]] {
		if s.CarNumber == carNumber {
			seatList = append(seatList, s)
		}
	}
	sort.Slice(seatList, func(i, j int) bool {
		if ri, rj := seatList[i].SeatRow, seatList[j].SeatRow; ri != rj {
			return ri < rj
		}
		return seatList[i].SeatColumn < seatList[j].SeatColumn
	})

	var seatInformationList []SeatInformation

	for _, seat := range seatList {

		s := SeatInformation{seat.SeatRow, seat.SeatColumn, seat.SeatClass, seat.IsSmokingSeat, false}

		seatReservationList := []SeatReservation{}

		query := `
SELECT s.*
FROM seat_reservations s, reservations r
WHERE
	r.date=? AND r.train_class=? AND r.train_name=? AND car_number=? AND seat_row=? AND seat_column=?
`

		err = dbx.SelectContext(r.Context(),
			&seatReservationList, query,
			date.Format("2006/01/02"),
			seat.TrainClass,
			trainName,
			seat.CarNumber,
			seat.SeatRow,
			seat.SeatColumn,
		)
		if err != nil {
			errorResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		resvIDs := []int{}
		{
			mResvIDs := make(map[int]bool)
			for _, sr := range seatReservationList {
				if !mResvIDs[sr.ReservationId] {
					mResvIDs[sr.ReservationId] = true
					resvIDs = append(resvIDs, sr.ReservationId)
				}
			}
		}

		resvMap := make(map[int]Reservation)
		if len(resvIDs) > 0 {
			query := "SELECT * FROM reservations WHERE reservation_id IN (?)"
			query, params, err := sqlx.In(query, resvIDs)
			if err != nil {
				log.Panic(err)
			}

			resvs := []Reservation{}
			err = dbx.SelectContext(r.Context(), &resvs, query, params...)
			if err != nil {
				log.Panic(err)
			}

			for _, r := range resvs {
				r.fillStationByID()
				resvMap[r.ReservationId] = r
			}
		}

		for _, seatReservation := range seatReservationList {
			reservation := resvMap[seatReservation.ReservationId]

			var departureStation, arrivalStation Station
			departureStation = stationMasterByName[reservation.Departure]
			arrivalStation = stationMasterByName[reservation.Arrival]

			if train.IsNobori {
				// 上り
				if toStation.ID < arrivalStation.ID && fromStation.ID <= arrivalStation.ID {
					// pass
				} else if toStation.ID >= departureStation.ID && fromStation.ID > departureStation.ID {
					// pass
				} else {
					s.IsOccupied = true
				}

			} else {
				// 下り

				if fromStation.ID < departureStation.ID && toStation.ID <= departureStation.ID {
					// pass
				} else if fromStation.ID >= arrivalStation.ID && toStation.ID > arrivalStation.ID {
					// pass
				} else {
					s.IsOccupied = true
				}

			}
		}

		seatInformationList = append(seatInformationList, s)
	}

	// 各号車の情報

	simpleCarInformationList := []SimpleCarInformation{}
	seats := make(map[int]Seat)
	for _, s := range seatMaster[trainClassID[trainClass]] {
		carnum := s.CarNumber
		if ss, ok := seats[carnum]; ok {
			if s.SeatRow < ss.SeatRow {
				seats[carnum] = s
			} else if s.SeatRow==ss.SeatRow && s.SeatColumn < ss.SeatColumn {
				seats[carnum] = s
			}
		} else {
			seats[carnum] = s
		}
	}

	i := 1
	for {
		seat, ok := seats[i]
		if !ok {
			break
		}
		simpleCarInformationList = append(simpleCarInformationList, SimpleCarInformation{i, seat.SeatClass})
		i = i + 1
	}

	c := CarInformation{date.Format("2006/01/02"), trainClass, trainName, carNumber, seatInformationList, simpleCarInformationList}
	resp, err := json.Marshal(c)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Write(resp)
}

func trainReservationHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	/*
		列車の席予約API　支払いはまだ
		POST /api/train/reserve
			{
				"date": "2020-12-31T07:57:00+09:00",
				"train_name": "183",
				"train_class": "中間",
				"car_number": 7,
				"is_smoking_seat": false,
				"seat_class": "reserved",
				"departure": "東京",
				"arrival": "名古屋",
				"child": 2,
				"adult": 1,
				"column": "A",
				"seats": [
					{
					"row": 3,
					"column": "B"
					},
						{
					"row": 4,
					"column": "C"
					}
				]
		}
		レスポンスで予約IDを返す
		reservationResponse(w http.ResponseWriter, errCode int, id int, ok bool, message string)
	*/

	// json parse
	req := new(TrainReservationRequest)
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "JSON parseに失敗しました")
		log.Println(err.Error())
		return
	}

	// 乗車日の日付表記統一
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	date, err := time.Parse(time.RFC3339, req.Date)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "時刻のparseに失敗しました")
		log.Println(err.Error())
	}
	date = date.In(jst)

	if !checkAvailableDate(date) {
		errorResponse(w, http.StatusNotFound, "予約可能期間外です")
		return
	}

	// 止まらない駅の予約を取ろうとしていないかチェックする
	// 列車データを取得
	tmas, ok := SelectTrainMasterByName(date, trainClassID[req.TrainClass], req.TrainName)
	if !ok {
		log.Printf("train (%v-%v-%v) not found", date, req.TrainClass, req.TrainName)
		errorResponse(w, http.StatusNotFound, "列車データがみつかりません")
		log.Println(err.Error())
		return
	}

	// 列車自体の駅IDを求める
	var departureStation, arrivalStation Station
	// Departure
	if departureStation, ok = stationMasterByName[tmas.StartStation]; !ok {
		errorResponse(w, http.StatusNotFound, "リクエストされた列車の始発駅データがみつかりません")
		log.Println("start staton (%s) not found", tmas.StartStation)
		return
	}
	// Arrive
	if arrivalStation, ok = stationMasterByName[tmas.LastStation]; !ok {
		errorResponse(w, http.StatusNotFound, "リクエストされた列車の終着駅データがみつかりません")
		log.Println("last station (%s) not found", tmas.LastStation)
		return
	}

	// リクエストされた乗車区間の駅IDを求める
	var fromStation, toStation Station
	// From
	if fromStation, ok = stationMasterByName[req.Departure]; !ok {
		errorResponse(w, http.StatusNotFound, fmt.Sprintf("乗車駅データがみつかりません %s", req.Departure))
		log.Println("departure station (%s) not found", req.Departure)
		return
	}

	// To
	if toStation, ok = stationMasterByName[req.Arrival]; !ok {
		errorResponse(w, http.StatusNotFound, fmt.Sprintf("降車駅データがみつかりません %s", req.Arrival))
		log.Println("arrival station (%s) not found", req.Arrival)
		return
	}

	switch req.TrainClass {
	case "最速":
		if !fromStation.IsStopExpress || !toStation.IsStopExpress {
			errorResponse(w, http.StatusBadRequest, "最速の止まらない駅です")
			return
		}
	case "中間":
		if !fromStation.IsStopSemiExpress || !toStation.IsStopSemiExpress {
			errorResponse(w, http.StatusBadRequest, "中間の止まらない駅です")
			return
		}
	case "遅いやつ":
		if !fromStation.IsStopLocal || !toStation.IsStopLocal {
			errorResponse(w, http.StatusBadRequest, "遅いやつの止まらない駅です")
			return
		}
	default:
		errorResponse(w, http.StatusBadRequest, "リクエストされた列車クラスが不明です")
		log.Println(err.Error())
		return
	}

	// 運行していない区間を予約していないかチェックする
	if tmas.IsNobori {
		if fromStation.ID > departureStation.ID || toStation.ID > departureStation.ID {
			errorResponse(w, http.StatusBadRequest, "リクエストされた区間に列車が運行していない区間が含まれています")
			return
		}
		if arrivalStation.ID >= fromStation.ID || arrivalStation.ID > toStation.ID {
			errorResponse(w, http.StatusBadRequest, "リクエストされた区間に列車が運行していない区間が含まれています")
			return
		}
	} else {
		if fromStation.ID < departureStation.ID || toStation.ID < departureStation.ID {
			errorResponse(w, http.StatusBadRequest, "リクエストされた区間に列車が運行していない区間が含まれています")
			return
		}
		if arrivalStation.ID <= fromStation.ID || arrivalStation.ID < toStation.ID {
			errorResponse(w, http.StatusBadRequest, "リクエストされた区間に列車が運行していない区間が含まれています")
			return
		}
	}

	/*
		あいまい座席検索
		seatsが空白の時に発動する
	*/
	switch len(req.Seats) {
	case 0:
		if req.SeatClass == "non-reserved" {
			break // non-reservedはそもそもあいまい検索もせずダミーのRow/Columnで予約を確定させる。
		}
		//当該列車・号車中の空き座席検索
		train, ok := SelectTrainMasterByName(date, trainClassID[req.TrainClass], req.TrainName)
		if !ok {
			errorResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		usableTrainClassList := getUsableTrainClassList(fromStation, toStation)
		usable := false
		for _, v := range usableTrainClassList {
			if v == train.TrainClass {
				usable = true
			}
		}
		if !usable {
			err = fmt.Errorf("invalid train_class")
			log.Print(err)
			errorResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		var query string

		req.Seats = []RequestSeat{} // 座席リクエスト情報は空に
		offset := rand.Int()
		for carnum := 1; carnum <= 16; carnum++ {
			carnum = carnum + offset%16 + 1
			seatList := []Seat{}
			for _, s := range seatMaster[trainClassID[req.TrainClass]] {
				if s.CarNumber == carnum && s.SeatClass == req.SeatClass && s.IsSmokingSeat==req.IsSmokingSeat {
					seatList = append(seatList, s)
				}
			}

			var seatInformationList []SeatInformation
			for _, seat := range seatList {
				s := SeatInformation{seat.SeatRow, seat.SeatColumn, seat.SeatClass, seat.IsSmokingSeat, false}
				seatReservationList := []SeatReservation{}
				query = "SELECT s.* FROM seat_reservations s, reservations r WHERE r.date=? AND r.train_class=? AND r.train_name=? AND car_number=? AND seat_row=? AND seat_column=?"
				err = dbx.SelectContext(r.Context(),
					&seatReservationList, query,
					date.Format("2006/01/02"),
					seat.TrainClass,
					req.TrainName,
					seat.CarNumber,
					seat.SeatRow,
					seat.SeatColumn,
				)
				if err != nil {
					errorResponse(w, http.StatusBadRequest, err.Error())
					return
				}

				for _, seatReservation := range seatReservationList {
					reservation, ok := getReservation(seatReservation.ReservationId)
					if !ok {
						log.Panicf("resv not found: %v", seatReservation.ReservationId)
					}

					var departureStation, arrivalStation Station
					departureStation = stationMasterByName[reservation.Departure]
					arrivalStation = stationMasterByName[reservation.Arrival]

					if train.IsNobori {
						// 上り
						if toStation.ID < arrivalStation.ID && fromStation.ID <= arrivalStation.ID {
							// pass
						} else if toStation.ID >= departureStation.ID && fromStation.ID > departureStation.ID {
							// pass
						} else {
							s.IsOccupied = true
						}
					} else {
						// 下り
						if fromStation.ID < departureStation.ID && toStation.ID <= departureStation.ID {
							// pass
						} else if fromStation.ID >= arrivalStation.ID && toStation.ID > arrivalStation.ID {
							// pass
						} else {
							s.IsOccupied = true
						}
					}
				}

				seatInformationList = append(seatInformationList, s)
			}

			// 曖昧予約席とその他の候補席を選出
			var seatnum int           // 予約する座席の合計数
			var reserved bool         // あいまい指定席確保済フラグ
			var vargue bool           // あいまい検索フラグ
			var VagueSeat RequestSeat // あいまい指定席保存用
			reserved = false
			vargue = true
			seatnum = (req.Adult + req.Child - 1) // 全体の人数からあいまい指定席分を引いておく
			if req.Column == "" {                 // A/B/C/D/Eを指定しなければ、空いている適当な指定席を取るあいまいモード
				seatnum = (req.Adult + req.Child) // あいまい指定せず大人＋小人分の座席を取る
				reserved = true                   // dummy
				vargue = false                    // dummy
			}
			var CandidateSeat RequestSeat
			CandidateSeats := []RequestSeat{}

			// シート分だけ回して予約できる席を検索
			var i int
			for _, seat := range seatInformationList {
				if seat.Column == req.Column && !seat.IsOccupied && !reserved && vargue { // あいまい席があいてる
					VagueSeat.Row = seat.Row
					VagueSeat.Column = seat.Column
					reserved = true
				} else if !seat.IsOccupied && i < seatnum { // 単に席があいてる
					CandidateSeat.Row = seat.Row
					CandidateSeat.Column = seat.Column
					CandidateSeats = append(CandidateSeats, CandidateSeat)
					i++
				}
			}

			if vargue && reserved { // あいまい席が見つかり、予約できそうだった
				req.Seats = append(req.Seats, VagueSeat) // あいまい予約席を追加
			}
			if i > 0 { // 候補席があった
				req.Seats = append(req.Seats, CandidateSeats...) // 予約候補席追加
			}

			if len(req.Seats) < req.Adult+req.Child {
				// リクエストに対して席数が足りてない
				// 次の号車にうつしたい
				log.Println("-----------------")
				log.Printf("現在検索中の車両: %d号車, リクエスト座席数: %d, 予約できそうな座席数: %d, 不足数: %d\n", carnum, req.Adult+req.Child, len(req.Seats), req.Adult+req.Child-len(req.Seats))
				log.Println("リクエストに対して座席数が不足しているため、次の車両を検索します。")
				req.Seats = []RequestSeat{}
				if carnum == 16 {
					log.Println("この新幹線にまとめて予約できる席数がなかったから検索をやめるよ")
					req.Seats = []RequestSeat{}
					break
				}
			}
			log.Printf("空き実績: %d号車 シート:%v 席数:%d\n", carnum, req.Seats, len(req.Seats))
			if len(req.Seats) >= req.Adult+req.Child {
				log.Println("予約情報に追加したよ")
				req.Seats = req.Seats[:req.Adult+req.Child]
				req.CarNumber = carnum
				break
			}
		}
		if len(req.Seats) == 0 {
			errorResponse(w, http.StatusNotFound, "あいまい座席予約ができませんでした。指定した席、もしくは1車両内に希望の席数をご用意できませんでした。")
			return
		}
	default:
		// 座席情報のValidate
		seatList := Seat{}
MASTERLOOP:
		for _, master := range seatMaster[trainClassID[req.TrainClass]] {
			if master.CarNumber != req.CarNumber || master.SeatClass != req.SeatClass {
				continue
			}
			for _, z := range req.Seats {
				if z.Column == master.SeatColumn && z.Row == master.SeatRow {
					seatList = master
					break MASTERLOOP
				}
			}
		}
		if seatList.CarNumber == 0 {
			errorResponse(w, http.StatusNotFound, "リクエストされた座席情報は存在しません。号車・喫煙席・座席クラスなど組み合わせを見直してください")
			log.Println("seat not found!!")
			return
		}
/*
		for _, z := range req.Seats {
			query := "SELECT * FROM seat_master WHERE train_class=? AND car_number=? AND seat_column=? AND seat_row=? AND seat_class=?"
			err = dbx.GetContext(r.Context(),
				&seatList, query,
				req.TrainClass,
				req.CarNumber,
				z.Column,
				z.Row,
				req.SeatClass,
			)
			if err != nil {
				errorResponse(w, http.StatusNotFound, "リクエストされた座席情報は存在しません。号車・喫煙席・座席クラスなど組み合わせを見直してください")
				log.Println(err.Error())
				return
			}
		}
*/
		break
	}

	// 当該列車・列車名の予約一覧取得
	tx := dbx.MustBegin()
	reservations := []Reservation{}
	query := "SELECT * FROM reservations WHERE date=? AND train_class=? AND train_name=? FOR UPDATE"
	err = tx.SelectContext(r.Context(),
		&reservations, query,
		date.Format("2006/01/02"),
		req.TrainClass,
		req.TrainName,
	)
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "列車予約情報の取得に失敗しました")
		log.Println(err.Error())
		return
	}

	for _, reservation := range reservations {
		if req.SeatClass == "non-reserved" {
			break
		}
		reservation.fillStationByID()
		// train_masterから列車情報を取得(上り・下りが分かる)
		tmas, ok := SelectTrainMasterByName(date, trainClassID[req.TrainClass], req.TrainName)
		if !ok {
			tx.Rollback()
			log.Printf("train (%v-%v-%v) not found", date, req.TrainClass, req.TrainName)
			errorResponse(w, http.StatusNotFound, "列車データがみつかりません")
			log.Println(err.Error())
			return
		}

		// 予約情報の乗車区間の駅IDを求める
		var reservedfromStation, reservedtoStation Station

		// From
		if reservedfromStation, ok = stationMasterByName[reservation.Departure]; !ok {
			tx.Rollback()
			errorResponse(w, http.StatusNotFound, "予約情報に記載された列車の乗車駅データがみつかりません")
			log.Println("departure station (%s) not found", reservation.Departure)
			return
		}

		// To
		if reservedtoStation, ok = stationMasterByName[reservation.Arrival]; !ok {
			tx.Rollback()
			errorResponse(w, http.StatusNotFound, "予約情報に記載された列車の降車駅データがみつかりません")
			log.Println("arrival station (%s) not found", reservation.Arrival)
			return
		}

		// 予約の区間重複判定
		secdup := false
		if tmas.IsNobori {
			// 上り
			if toStation.ID < reservedtoStation.ID && fromStation.ID <= reservedtoStation.ID {
				// pass
			} else if toStation.ID >= reservedfromStation.ID && fromStation.ID > reservedfromStation.ID {
				// pass
			} else {
				secdup = true
			}
		} else {
			// 下り
			if fromStation.ID < reservedfromStation.ID && toStation.ID <= reservedfromStation.ID {
				// pass
			} else if fromStation.ID >= reservedtoStation.ID && toStation.ID > reservedtoStation.ID {
				// pass
			} else {
				secdup = true
			}
		}

		if secdup {

			// 区間重複の場合は更に座席の重複をチェックする
			SeatReservations := []SeatReservation{}
			query := "SELECT * FROM seat_reservations WHERE reservation_id=? FOR UPDATE"
			err = tx.SelectContext(r.Context(),
				&SeatReservations, query,
				reservation.ReservationId,
			)
			if err != nil {
				tx.Rollback()
				errorResponse(w, http.StatusInternalServerError, "座席予約情報の取得に失敗しました")
				log.Println(err.Error())
				return
			}

			for _, v := range SeatReservations {
				for _, seat := range req.Seats {
					if v.CarNumber == req.CarNumber && v.SeatRow == seat.Row && v.SeatColumn == seat.Column {
						tx.Rollback()
						log.Println("Duplicated ", reservation)
						errorResponse(w, http.StatusBadRequest, "リクエストに既に予約された席が含まれています")
						return
					}
				}
			}
		}
	}
	// 3段階の予約前チェック終わり

	// 自由席は強制的にSeats情報をダミーにする（自由席なのに席指定予約は不可）
	if req.SeatClass == "non-reserved" {
		req.Seats = []RequestSeat{}
		dummySeat := RequestSeat{}
		req.CarNumber = 0
		for num := 0; num < req.Adult+req.Child; num++ {
			dummySeat.Row = 0
			dummySeat.Column = ""
			req.Seats = append(req.Seats, dummySeat)
		}
	}

	// 運賃計算
	var fare int
	switch req.SeatClass {
	case "premium":
		fare, err = fareCalc(ctx, date, fromStation.ID, toStation.ID, req.TrainClass, "premium")
		if err != nil {
			tx.Rollback()
			errorResponse(w, http.StatusBadRequest, err.Error())
			log.Println("fareCalc " + err.Error())
			return
		}
	case "reserved":
		fare, err = fareCalc(ctx, date, fromStation.ID, toStation.ID, req.TrainClass, "reserved")
		if err != nil {
			tx.Rollback()
			errorResponse(w, http.StatusBadRequest, err.Error())
			log.Println("fareCalc " + err.Error())
			return
		}
	case "non-reserved":
		fare, err = fareCalc(ctx, date, fromStation.ID, toStation.ID, req.TrainClass, "non-reserved")
		if err != nil {
			tx.Rollback()
			errorResponse(w, http.StatusBadRequest, err.Error())
			log.Println("fareCalc " + err.Error())
			return
		}
	default:
		tx.Rollback()
		errorResponse(w, http.StatusBadRequest, "リクエストされた座席クラスが不明です")
		return
	}
	sumFare := (req.Adult * fare) + (req.Child*fare)/2

	// userID取得。ログインしてないと怒られる。
	user, errCode, errMsg := getUser(r)
	if errCode != http.StatusOK {
		tx.Rollback()
		errorResponse(w, errCode, errMsg)
		log.Printf("%s", errMsg)
		return
	}

	//予約ID発行と予約情報登録
	query = "INSERT INTO `reservations` (`user_id`, `date`, `train_class`, `train_name`, `departure`, `arrival`, `status`, `payment_id`, `adult`, `child`, `amount`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	result, err := tx.Exec(
		query,
		user.ID,
		date.Format("2006/01/02"),
		req.TrainClass,
		req.TrainName,
		stationMasterByName[req.Departure].ID,
		stationMasterByName[req.Arrival].ID,
		"requesting",
		"a",
		req.Adult,
		req.Child,
		sumFare,
	)
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusBadRequest, "予約の保存に失敗しました。"+err.Error())
		log.Println(err.Error())
		return
	}
	id, err := result.LastInsertId() //予約ID
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "予約IDの取得に失敗しました")
		log.Println(err.Error())
		return
	}
	log.Printf("insert reservations: user_id=%v, resv_id=%v", user.ID, id)

	var uid int = int(user.ID)
	inserted := Reservation{
		ReservationId: int(id),
		UserId:        &uid,
		Date:          &date,
		TrainClass:    req.TrainClass,
		TrainName:     req.TrainName,
		DepartureID:   stationMasterByName[req.Departure].ID,
		Departure:     req.Departure,
		ArrivalID:     stationMasterByName[req.Arrival].ID,
		Arrival:       req.Arrival,
		Status:        "requesting",
		PaymentId:     "a",
		Adult:         req.Adult,
		Child:         req.Child,
		Amount:        sumFare,
	}

	//席の予約情報登録
	//reservationsレコード1に対してseat_reservationstが1以上登録される
	query = "INSERT INTO `seat_reservations` (`reservation_id`, `car_number`, `seat_row`, `seat_column`) VALUES (?, ?, ?, ?)"
	for _, v := range req.Seats {
		_, err = tx.Exec(
			query,
			id,
			req.CarNumber,
			v.Row,
			v.Column,
		)
		if err != nil {
			tx.Rollback()
			errorResponse(w, http.StatusInternalServerError, "座席予約の登録に失敗しました")
			log.Println(err.Error())
			return
		}
	}

	rr := TrainReservationResponse{
		ReservationId: id,
		Amount:        sumFare,
		IsOk:          true,
	}
	response, err := json.Marshal(rr)
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "レスポンスの生成に失敗しました")
		log.Println(err.Error())
		return
	}
	tx.Commit()
	insertReservation(inserted)
	w.Write(response)
}

func reservationPaymentHandler(w http.ResponseWriter, r *http.Request) {
	/*
		支払い及び予約確定API
		POST /api/train/reservation/commit
		{
			"card_token": "161b2f8f-791b-4798-42a5-ca95339b852b",
			"reservation_id": "1"
		}

		前段でフロントがクレカ非保持化対応用のpayment-APIを叩き、card_tokenを手に入れている必要がある
		レスポンスは成功か否かのみ返す
	*/

	// json parse
	req := new(ReservationPaymentRequest)
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "JSON parseに失敗しました")
		log.Println(err.Error())
		return
	}

	tx := dbx.MustBegin()

	// 予約IDで検索
	reservation := Reservation{}
	query := "SELECT * FROM reservations WHERE reservation_id=?"
	err = tx.GetContext(r.Context(),
		&reservation, query,
		req.ReservationId,
	)
	if err == sql.ErrNoRows {
		tx.Rollback()
		errorResponse(w, http.StatusNotFound, "予約情報がみつかりません")
		log.Println(err.Error())
		return
	}
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "予約情報の取得に失敗しました")
		log.Println(err.Error())
		return
	}

	// 支払い前のユーザチェック。本人以外のユーザの予約を支払ったりキャンセルできてはいけない。
	user, errCode, errMsg := getUser(r)
	if errCode != http.StatusOK {
		tx.Rollback()
		errorResponse(w, errCode, errMsg)
		log.Printf("%s", errMsg)
		return
	}
	if int64(*reservation.UserId) != user.ID {
		tx.Rollback()
		errorResponse(w, http.StatusForbidden, "他のユーザIDの支払いはできません")
		log.Println(err.Error())
		return
	}

	// 予約情報の支払いステータス確認
	switch reservation.Status {
	case "done":
		tx.Rollback()
		errorResponse(w, http.StatusForbidden, "既に支払いが完了している予約IDです")
		return
	default:
		break
	}

	// 決済する
	payInfo := PaymentInformationRequest{req.CardToken, req.ReservationId, reservation.Amount}
	j, err := json.Marshal(PaymentInformation{PayInfo: payInfo})
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "JSON Marshalに失敗しました")
		log.Println(err.Error())
		return
	}

	payment_api := os.Getenv("PAYMENT_API")
	if payment_api == "" {
		payment_api = "http://payment:5000"
	}

	preq, err := http.NewRequestWithContext(r.Context(), "POST", payment_api+"/payment", bytes.NewBuffer(j))
	if err != nil {
		log.Panic(err)
	}
	resp, err := http.DefaultClient.Do(preq)
	if err != nil {
		tx.Rollback()
		errorResponse(w, resp.StatusCode, "HTTP POSTに失敗しました")
		log.Println(err.Error())
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "レスポンスの読み込みに失敗しました")
		log.Println(err.Error())
		return
	}

	// リクエスト失敗
	if resp.StatusCode != http.StatusOK {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "決済に失敗しました。カードトークンや支払いIDが間違っている可能性があります")
		log.Println(resp.StatusCode)
		return
	}

	// リクエスト取り出し
	output := PaymentResponse{}
	err = json.Unmarshal(body, &output)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "JSON parseに失敗しました")
		log.Println(err.Error())
		return
	}

	// 予約情報の更新
	query = "UPDATE reservations SET status=?, payment_id=? WHERE reservation_id=?"
	_, err = tx.Exec(
		query,
		"done",
		output.PaymentId,
		req.ReservationId,
	)
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "予約情報の更新に失敗しました")
		log.Println(err.Error())
		return
	}

	rr := ReservationPaymentResponse{
		IsOk: true,
	}
	response, err := json.Marshal(rr)
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "レスポンスの生成に失敗しました")
		log.Println(err.Error())
		return
	}
	tx.Commit()
	w.Write(response)
}

func getAuthHandler(w http.ResponseWriter, r *http.Request) {

	// userID取得
	user, errCode, errMsg := getUser(r)
	if errCode != http.StatusOK {
		errorResponse(w, errCode, errMsg)
		log.Printf("%s", errMsg)
		return
	}

	resp := AuthResponse{user.Email}
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

func signUpHandler(w http.ResponseWriter, r *http.Request) {
	/*
		ユーザー登録
		POST /auth/signup
	*/

	defer r.Body.Close()
	buf, _ := ioutil.ReadAll(r.Body)

	user := User{}
	json.Unmarshal(buf, &user)

	// TODO: validation

	salt := make([]byte, 1024)
	_, err := crand.Read(salt)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "salt generator error")
		return
	}
	superSecurePassword := pbkdf2.Key([]byte(user.Password), salt, 100, 256, sha256.New)

	_, err = dbx.Exec(
		"INSERT INTO `users` (`email`, `salt`, `super_secure_password`) VALUES (?, ?, ?)",
		user.Email,
		salt,
		superSecurePassword,
	)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "user registration failed")
		return
	}

	messageResponse(w, "registration complete")
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	/*
		ログイン
		POST /auth/login
	*/

	defer r.Body.Close()
	buf, _ := ioutil.ReadAll(r.Body)

	postUser := User{}
	json.Unmarshal(buf, &postUser)

	user := User{}
	query := "SELECT * FROM users WHERE email=?"
	err := dbx.GetContext(r.Context(), &user, query, postUser.Email)
	if err == sql.ErrNoRows {
		errorResponse(w, http.StatusForbidden, "authentication failed")
		return
	}
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	challengePassword := pbkdf2.Key([]byte(postUser.Password), user.Salt, 100, 256, sha256.New)

	if !bytes.Equal(user.HashedPassword, challengePassword) {
		errorResponse(w, http.StatusForbidden, "authentication failed")
		return
	}

	session := getSession(r)

	session.Values["user_id"] = user.ID
	if err = session.Save(r, w); err != nil {
		log.Print(err)
		errorResponse(w, http.StatusInternalServerError, "session error")
		return
	}
	messageResponse(w, "autheticated")
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	/*
		ログアウト
		POST /auth/logout
	*/

	session := getSession(r)

	session.Values["user_id"] = 0
	if err := session.Save(r, w); err != nil {
		log.Print(err)
		errorResponse(w, http.StatusInternalServerError, "session error")
		return
	}
	messageResponse(w, "logged out")
}

func makeReservationResponse(ctx context.Context, reservation Reservation) (ReservationResponse, error) {

	reservationResponse := ReservationResponse{}
	reservation.fillStationByID()

	var departure, arrival string
	err := dbx.GetContext(ctx,
		&departure,
		"SELECT departure FROM train_timetable_master WHERE date=? AND train_class=? AND train_name=? AND station=?",
		reservation.Date.Format("2006/01/02"), reservation.TrainClass, reservation.TrainName, reservation.Departure,
	)
	if err != nil {
		return reservationResponse, err
	}
	err = dbx.GetContext(ctx,
		&arrival,
		"SELECT arrival FROM train_timetable_master WHERE date=? AND train_class=? AND train_name=? AND station=?",
		reservation.Date.Format("2006/01/02"), reservation.TrainClass, reservation.TrainName, reservation.Arrival,
	)
	if err != nil {
		return reservationResponse, err
	}

	reservationResponse.ReservationId = reservation.ReservationId
	reservationResponse.Date = reservation.Date.Format("2006/01/02")
	reservationResponse.Amount = reservation.Amount
	reservationResponse.Adult = reservation.Adult
	reservationResponse.Child = reservation.Child
	reservationResponse.Departure = reservation.Departure
	reservationResponse.Arrival = reservation.Arrival
	reservationResponse.TrainClass = reservation.TrainClass
	reservationResponse.TrainName = reservation.TrainName
	reservationResponse.DepartureTime = departure
	reservationResponse.ArrivalTime = arrival

	query := "SELECT * FROM seat_reservations WHERE reservation_id=?"
	err = dbx.SelectContext(ctx, &reservationResponse.Seats, query, reservation.ReservationId)

	// 1つの予約内で車両番号は全席同じ
	reservationResponse.CarNumber = reservationResponse.Seats[0].CarNumber

	if reservationResponse.Seats[0].CarNumber == 0 {
		reservationResponse.SeatClass = "non-reserved"
	} else {
		// 座席種別を取得
		seat := Seat{}
		err := fmt.Errorf("seat not found")
		for _, s := range seatMaster[trainClassID[reservation.TrainClass]] {
			if s.CarNumber == reservationResponse.CarNumber &&
				s.SeatColumn == reservationResponse.Seats[0].SeatColumn &&
				s.SeatRow == reservationResponse.Seats[0].SeatRow {
				seat = s
				err = nil
				break
			}
		}
		if err != nil {
			return reservationResponse, err
		}
		reservationResponse.SeatClass = seat.SeatClass
	}

	for i, v := range reservationResponse.Seats {
		// omit
		v.ReservationId = 0
		v.CarNumber = 0
		reservationResponse.Seats[i] = v
	}
	return reservationResponse, nil
}

func userReservationsHandler(w http.ResponseWriter, r *http.Request) {
	/*
		ログイン
		POST /auth/login
	*/
	user, errCode, errMsg := getUser(r)
	if errCode != http.StatusOK {
		errorResponse(w, errCode, errMsg)
		return
	}
	reservationList := []Reservation{}

	ctx := r.Context()
	query := "SELECT * FROM reservations WHERE user_id=?"
	err := dbx.SelectContext(ctx, &reservationList, query, user.ID)
	if err != nil {
		log.Printf("reservation not found: user_id=%v", user.ID)
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	reservationResponseList := []ReservationResponse{}

	for _, r := range reservationList {
		r.fillStationByID()
		//log.Printf(" resv: %+v", r)
		res, err := makeReservationResponse(ctx, r)
		if err != nil {
			errorResponse(w, http.StatusBadRequest, err.Error())
			log.Println("makeReservationResponse()", err)
			return
		}
		reservationResponseList = append(reservationResponseList, res)
	}

	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(reservationResponseList)
}

func userReservationResponseHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, errCode, errMsg := getUser(r)
	if errCode != http.StatusOK {
		errorResponse(w, errCode, errMsg)
		return
	}
	itemIDStr := pat.Param(r, "item_id")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil || itemID <= 0 {
		errorResponse(w, http.StatusBadRequest, "incorrect item id")
		return
	}

	reservation := Reservation{}
	query := "SELECT * FROM reservations WHERE reservation_id=? AND user_id=?"
	err = dbx.GetContext(ctx, &reservation, query, itemID, user.ID)
	if err == sql.ErrNoRows {
		log.Printf("resv not found: user_id=%v id=%v", user.ID, itemID)
		errorResponse(w, http.StatusNotFound, "Reservation not found")
		return
	}
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	reservation.fillStationByID()
	//log.Printf("resv found: %+v", reservation)
	reservationResponse, err := makeReservationResponse(ctx, reservation)

	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		log.Println("makeReservationResponse() ", err)
		return
	}

	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(reservationResponse)
}

func userReservationCancelHandler(w http.ResponseWriter, r *http.Request) {
	user, errCode, errMsg := getUser(r)
	if errCode != http.StatusOK {
		errorResponse(w, errCode, errMsg)
		return
	}
	itemIDStr := pat.Param(r, "item_id")
	itemID, err := strconv.ParseInt(itemIDStr, 10, 64)
	if err != nil || itemID <= 0 {
		errorResponse(w, http.StatusBadRequest, "incorrect item id")
		return
	}

	tx := dbx.MustBegin()

	reservation := Reservation{}
	query := "SELECT * FROM reservations WHERE reservation_id=? AND user_id=?"
	err = tx.GetContext(r.Context(), &reservation, query, itemID, user.ID)
	log.Println("CANCEL", reservation, itemID, user.ID)
	if err == sql.ErrNoRows {
		tx.Rollback()
		errorResponse(w, http.StatusBadRequest, "reservations naiyo")
		return
	}
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "予約情報の検索に失敗しました")
	}

	switch reservation.Status {
	case "rejected":
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "何らかの理由により予約はRejected状態です")
		return
	case "done":
		// 支払いをキャンセルする
		payInfo := CancelPaymentInformationRequest{reservation.PaymentId}
		j, err := json.Marshal(payInfo)
		if err != nil {
			tx.Rollback()
			errorResponse(w, http.StatusInternalServerError, "JSON Marshalに失敗しました")
			log.Println(err.Error())
			return
		}

		payment_api := os.Getenv("PAYMENT_API")
		if payment_api == "" {
			payment_api = "http://payment:5000"
		}

		client := http.DefaultClient
		req, err := http.NewRequestWithContext(r.Context(), "DELETE", payment_api+"/payment/"+reservation.PaymentId, bytes.NewBuffer(j))
		if err != nil {
			tx.Rollback()
			errorResponse(w, http.StatusInternalServerError, "HTTPリクエストの作成に失敗しました")
			log.Println(err.Error())
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			tx.Rollback()
			errorResponse(w, resp.StatusCode, "HTTP DELETEに失敗しました")
			log.Println(err.Error())
			return
		}
		defer resp.Body.Close()

		// リクエスト失敗
		if resp.StatusCode != http.StatusOK {
			tx.Rollback()
			errorResponse(w, http.StatusInternalServerError, "決済のキャンセルに失敗しました")
			log.Println(resp.StatusCode)
			return
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			tx.Rollback()
			errorResponse(w, http.StatusInternalServerError, "レスポンスの読み込みに失敗しました")
			log.Println(err.Error())
			return
		}

		// リクエスト取り出し
		output := CancelPaymentInformationResponse{}
		err = json.Unmarshal(body, &output)
		if err != nil {
			errorResponse(w, http.StatusInternalServerError, "JSON parseに失敗しました")
			log.Println(err.Error())
			return
		}
		log.Println(output)
	default:
		// pass(requesting状態のものはpayment_id無いので叩かない)
	}

	query = "DELETE FROM reservations WHERE reservation_id=? AND user_id=?"
	_, err = tx.Exec(query, itemID, user.ID)
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	query = "DELETE FROM seat_reservations WHERE reservation_id=?"
	_, err = tx.Exec(query, itemID)
	if err == sql.ErrNoRows {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, "seat naiyo")
		// errorResponse(w, http.Status, "authentication failed")
		return
	}
	if err != nil {
		tx.Rollback()
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	tx.Commit()
	deleteReservation(int(itemID))
	messageResponse(w, "cancell complete")
}

func initializeHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("initializeHandler")
	/*
		initialize
	*/

	log.Println("truncating...")
	dbx.Exec("TRUNCATE seat_reservations")
	dbx.Exec("TRUNCATE reservations")
	dbx.Exec("TRUNCATE users")

	initReservationCache()

	// TODO: 最後に外す
	log.Println("initialize profiler")
	StartProfile(time.Minute)

	log.Println("done")
	resp := InitializeResponse{
		availableDays,
		"golang",
	}
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

func settingsHandler(w http.ResponseWriter, r *http.Request) {
	payment_api := os.Getenv("PAYMENT_API")
	if payment_api == "" {
		payment_api = "http://localhost:5000"
	}

	settings := Settings{
		PaymentAPI: payment_api,
	}

	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(settings)
}

func dummyHandler(w http.ResponseWriter, r *http.Request) {
	messageResponse(w, "ok")
}

func main() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
	// MySQL関連のお膳立て
	var err error

	host := os.Getenv("MYSQL_HOSTNAME")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("MYSQL_PORT")
	if port == "" {
		port = "3306"
	}
	_, err = strconv.Atoi(port)
	if err != nil {
		port = "3306"
	}
	user := os.Getenv("MYSQL_USER")
	if user == "" {
		user = "isutrain"
	}
	dbname := os.Getenv("MYSQL_DATABASE")
	if dbname == "" {
		dbname = "isutrain"
	}
	password := os.Getenv("MYSQL_PASSWORD")
	if password == "" {
		password = "isutrain"
	}

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local&interpolateParams=true",
		user,
		password,
		host,
		port,
		dbname,
	)

	dbx, err = sqlx.Open(tracedDriver("mysql"), dsn)
	if err != nil {
		log.Fatalf("failed to connect to DB: %s.", err.Error())
	}
	defer dbx.Close()
	dbx.SetMaxOpenConns(64)
	dbx.SetMaxIdleConns(64)
	dbx.SetConnMaxLifetime(time.Minute * 3)

	for {
		_, err := dbx.Exec("select 42")
		if err != nil {
			log.Println(err)
			time.Sleep(time.Second)
		}
		break
	}

	initTrainMaster()
	initSeatMaster()
	initReservationCache()

	// HTTP

	mux := goji.NewMux()

	mux.HandleFunc(pat.Post("/initialize"), initializeHandler)
	mux.HandleFunc(pat.Get("/api/settings"), settingsHandler)

	// 予約関係
	mux.HandleFunc(pat.Get("/api/stations"), getStationsHandler)
	mux.HandleFunc(pat.Get("/api/train/search"), trainSearchHandler)
	mux.HandleFunc(pat.Get("/api/train/seats"), trainSeatsHandler)
	mux.HandleFunc(pat.Post("/api/train/reserve"), trainReservationHandler)
	mux.HandleFunc(pat.Post("/api/train/reservation/commit"), reservationPaymentHandler)

	// 認証関連
	mux.HandleFunc(pat.Get("/api/auth"), getAuthHandler)
	mux.HandleFunc(pat.Post("/api/auth/signup"), signUpHandler)
	mux.HandleFunc(pat.Post("/api/auth/login"), loginHandler)
	mux.HandleFunc(pat.Post("/api/auth/logout"), logoutHandler)
	mux.HandleFunc(pat.Get("/api/user/reservations"), userReservationsHandler)
	mux.HandleFunc(pat.Get("/api/user/reservations/:item_id"), userReservationResponseHandler)
	mux.HandleFunc(pat.Post("/api/user/reservations/:item_id/cancel"), userReservationCancelHandler)

	//log.Println(banner)
	err = http.ListenAndServe(":8000", withTrace(mux))

	log.Fatal(err)
}
