package main

func asString(v any) string {
	s, _ := v.(string)
	return s
}
