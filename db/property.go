package db

type Property struct {
	Key   string      `json:"key,identity"`
	Value interface{} `json:"value"`
}
