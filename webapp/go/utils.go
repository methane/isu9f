package main

import (
	"context"
	"fmt"
	"time"
)

func checkAvailableDate(date time.Time) bool {
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	t := time.Date(2020, 1, 1, 0, 0, 0, 0, jst)
	t = t.AddDate(0, 0, availableDays)

	return date.Before(t)
}

func getUsableTrainClassIDList(fromStation, toStation Station) []int {
	usable := []int{}
	if fromStation.IsStopExpress && toStation.IsStopExpress {
		usable = append(usable, 0)
	}
	if fromStation.IsStopSemiExpress && toStation.IsStopSemiExpress {
		usable = append(usable, 1)
	}
	usable = append(usable, 2)
	return usable
}

func getUsableTrainClassList(fromStation Station, toStation Station) []string {
	usable := map[string]string{}

	for key, value := range TrainClassMap {
		usable[key] = value
	}

	if !fromStation.IsStopExpress {
		delete(usable, "express")
	}
	if !fromStation.IsStopSemiExpress {
		delete(usable, "semi_express")
	}
	if !fromStation.IsStopLocal {
		delete(usable, "local")
	}

	if !toStation.IsStopExpress {
		delete(usable, "express")
	}
	if !toStation.IsStopSemiExpress {
		delete(usable, "semi_express")
	}
	if !toStation.IsStopLocal {
		delete(usable, "local")
	}

	ret := []string{}
	for _, v := range usable {
		ret = append(ret, v)
	}

	return ret
}

func (train Train) getAvailableSeats(ctx context.Context, fromStation Station, toStation Station) ([]Seat, error) {
	// 指定種別の空き座席を返す

	var err error

	// 全ての座席を取得する
	seatList := seatMaster[train.TrainClassID]

	availableSeatMap := map[string]Seat{}
	for _, seat := range seatList {
		availableSeatMap[fmt.Sprintf("%d_%d_%s", seat.CarNumber, seat.SeatRow, seat.SeatColumn)] = seat
	}

	// すでに取られている予約を取得する
	query := `
	SELECT sr.reservation_id, sr.car_number, sr.seat_row, sr.seat_column
	FROM seat_reservations sr, reservations r
	WHERE
		r.reservation_id=sr.reservation_id AND r.train_class = ?
	`

	if train.IsNobori {
		query += "AND ((r.arrival < ? AND ? <= r.departure) OR (r.arrival < ? AND ? <= r.departure) OR (? < r.arrival AND r.departure < ?))"
	} else {
		query += "AND ((r.departure <= ? AND ? < r.arrival) OR (r.departure <= ? AND ? < r.arrival) OR (r.arrival < ? AND ? < r.departure))"
	}

	seatReservationList := []SeatReservation{}
	err = dbx.SelectContext(ctx, &seatReservationList, query, train.TrainClass, fromStation.ID, fromStation.ID, toStation.ID, toStation.ID, fromStation.ID, toStation.ID)
	if err != nil {
		return nil, err
	}

	for _, seatReservation := range seatReservationList {
		key := fmt.Sprintf("%d_%d_%s", seatReservation.CarNumber, seatReservation.SeatRow, seatReservation.SeatColumn)
		delete(availableSeatMap, key)
	}

	ret := make([]Seat, 0, len(availableSeatMap))
	for _, seat := range availableSeatMap {
		ret = append(ret, seat)
	}
	return ret, nil
}
