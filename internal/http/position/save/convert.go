package save

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/http/jsonbody"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

func decodeInput(w http.ResponseWriter, r *http.Request, tripID uuid.UUID) (model.SavePositionInput, error) {
	body, err := jsonbody.Read(w, r)
	if err != nil {
		return model.SavePositionInput{}, err
	}
	fields, err := jsonbody.Object(body, "", "coordinates", "recorded_at")
	if err != nil {
		return model.SavePositionInput{}, err
	}
	input := model.SavePositionInput{TripID: tripID}
	values, err := jsonbody.Coordinates(fields, "coordinates", &input.Coordinates)
	if err != nil {
		return model.SavePositionInput{}, err
	}
	values = append(values, jsonbody.Field{
		Path:     "recorded_at",
		Raw:      fields["recorded_at"],
		Expected: "an RFC 3339 date-time",
		Dest:     &input.RecordedAt,
	})
	if err := jsonbody.Decode(values); err != nil {
		return model.SavePositionInput{}, err
	}
	return input, nil
}
