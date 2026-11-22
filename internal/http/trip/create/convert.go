package create

import (
	"net/http"
	"slices"

	"github.com/KirillKrainov22/tripgo/internal/http/jsonbody"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

func decodeInput(w http.ResponseWriter, r *http.Request) (model.CreateTripInput, error) {
	body, err := jsonbody.Read(w, r)
	if err != nil {
		return model.CreateTripInput{}, err
	}
	fields, err := jsonbody.Object(body, "", "user_id", "driver_id", "start_point", "end_point", "price")
	if err != nil {
		return model.CreateTripInput{}, err
	}
	var input model.CreateTripInput
	start, err := jsonbody.Coordinates(fields, "start_point", &input.Data.StartPoint)
	if err != nil {
		return model.CreateTripInput{}, err
	}
	end, err := jsonbody.Coordinates(fields, "end_point", &input.Data.EndPoint)
	if err != nil {
		return model.CreateTripInput{}, err
	}
	values := slices.Concat([]jsonbody.Field{
		{Path: "user_id", Raw: fields["user_id"], Expected: "a UUID", Dest: &input.Data.UserID},
		{Path: "driver_id", Raw: fields["driver_id"], Expected: "a UUID", Dest: &input.Data.DriverID},
		{Path: "price", Raw: fields["price"], Expected: "an integer", Dest: &input.Data.Price},
	}, start, end)
	if err := jsonbody.Decode(values); err != nil {
		return model.CreateTripInput{}, err
	}
	return input, input.Data.Validate()
}
