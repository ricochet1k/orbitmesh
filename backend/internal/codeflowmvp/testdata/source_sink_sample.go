package sample

type responseWriter struct{}

func (responseWriter) Write(b []byte) {
}

type request struct{}

func (request) FormValue(name string) string {
	return ""
}

type dbConn struct{}

func (dbConn) Exec(query string) {
}

func vulnerable(w responseWriter, r request, db dbConn) {
	q := r.FormValue("q")
	db.Exec("SELECT * FROM items WHERE id = " + q)
	w.Write([]byte(q))
}

func safe(w responseWriter, r request, db dbConn) {
	q := r.FormValue("q")
	strconv.Atoi(q)
	db.Exec("SELECT * FROM items WHERE id = " + q)
	w.Write([]byte(q))
}
