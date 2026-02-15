package innertube

import "encoding/json"

func MarshalRequest(req *PlayerRequest) ([]byte, error) {
	return json.Marshal(req)
}
