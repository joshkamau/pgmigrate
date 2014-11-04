package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/lib/pq"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const DEFAULT_FILE_PERMISSION = 0644
const DEFAULT_DIR_PERMISSION = 0755

type config struct {
	DbHost             string `json:"dbHost"`
	DbName             string `json:"dbName"`
	DbUsername         string `json:"dbUsername"`
	DbPassword         string `json:"dbPassword"`
	MigrationTableName string `json:"migrationTableName"`
}

type migration struct {
	Description string
	Timestamp   int64
	DoScript    string
	UndoScript  string
	IsApplied   bool
}

type migrations []migration

func (ms migrations) Less(i, j int) bool {
	return ms[i].Timestamp < ms[j].Timestamp
}

func (ms migrations) Swap(i, j int) {
	ms[i], ms[j] = ms[j], ms[i]
}

func (ms migrations) Len() int {
	return len(ms)
}

var migrationTpl = `-- {{.Description}} --
-- @DO sql script --


-- @UNDO sql script --


`

//run the do script
func (m *migration) Do() {
	c := GetConfig()
	ExecuteSql(m.DoScript)

	insertSql := fmt.Sprintf("INSERT INTO %s (timestamp, description) VALUES ($1, $2)", c.MigrationTableName)
	db := getDb()
	_, err := db.Exec(insertSql, m.Timestamp, m.Description)
	if err != nil {
		panic(err)
	}
}

//run the undo script
func (m *migration) Undo() {
	c := GetConfig()
	ExecuteSql(m.UndoScript)

	deleteSql := fmt.Sprintf("DELETE FROM %s WHERE timestamp = $1", c.MigrationTableName)
	_, err := db.Exec(deleteSql, m.Timestamp)
	if err != nil {
		panic(err)
	}
}

//write migration to file
func (m *migration) WriteToFile() error {
	tpl, err := template.New("MigrationTemplate").Parse(migrationTpl)
	if err != nil {
		return err
	}
	var templ bytes.Buffer
	tpl.Execute(&templ, m)
	templBytes := templ.Bytes()
	templAbsPath, err := filepath.Abs(".")
	if err != nil {
		return err
	}

	tempPathNames := strings.Split(m.Description, " ")
	templPath := templAbsPath + "/scripts/" + strconv.FormatInt(m.Timestamp, 10) + "_" + strings.Join(tempPathNames, "_") + ".sql"

	err = ioutil.WriteFile(templPath, templBytes, DEFAULT_FILE_PERMISSION)
	if err != nil {
		return err
	}

	return nil
}

var db *sql.DB

//read config file or panic in case of error
func MustReadConfig() *config {
	configPath, err := filepath.Abs("./pgmigrate.json")
	if err != nil {
		panic(err)
	}
	configBytes, err := ioutil.ReadFile(configPath)
	if err != nil {
		panic(err)
	}
	var c config
	json.Unmarshal(configBytes, &c)
	return &c
}

var conf *config

//Get the configuration, reads from file if the configuration was not already loaded
func GetConfig() *config {
	if conf == nil {
		c := MustReadConfig()
		conf = c
	}
	return conf
}

//Creates a db connection if one was not created before.
func getDb() *sql.DB {
	c := GetConfig()
	if db == nil {
		connStr := fmt.Sprintf("dbname=%s user=%s password=%s sslmode=disable", c.DbName, c.DbUsername, c.DbPassword)
		newDb, err := sql.Open("postgres", connStr)
		if err != nil {
			panic(err)
		}
		db = newDb
	}
	return db
}

//Executes a query without parameters
func ExecuteSql(query string) {
	db := getDb()
	_, err := db.Exec(query)
	if err != nil {
		log.Fatalln(err)
		return
	}
}

//Checks if a migration is already applied
func IsMigrationApplied(m *migration) bool {
	var count int
	conf := GetConfig()
	db := getDb()
	err := db.QueryRow("SELECT COUNT(*) as count FROM "+conf.MigrationTableName+" WHERE timestamp = $1", m.Timestamp).Scan(&count)
	if err != nil {
		panic(err)
	}

	if count > 0 {
		return true
	}
	return false
}

//Mark migration as either applied or not
func SetMigrationStatus(m *migration) {
	if IsMigrationApplied(m) {
		m.IsApplied = true
	}
}

//Reads a migration from file
func ReadMigration(filename string) *migration {
	migrationBytes, err := ioutil.ReadFile("./scripts/" + filename)
	if err != nil {
		log.Fatal(err)
	}
	migrationStr := string(migrationBytes)
	lines := strings.Split(migrationStr, "\n")
	var doScript string
	var undoScript string
	doing := true
	for _, line := range lines {
		if strings.Contains(line, "-- @DO") {
			doing = true
		}
		if strings.Contains(line, "-- @UNDO") {
			doing = false
		}
		if doing {
			doScript = doScript + line + "\n"
		} else {
			undoScript = undoScript + line + "\n"
		}
	}

	//get the timestamp part
	re := regexp.MustCompile("[0-9]+")
	matches := re.FindAllString(filename, 1)

	var timestamp int64
	if len(matches) > 0 {
		timestamp, err = strconv.ParseInt(matches[0], 10, 64)
		if err != nil {
			panic(err)
		}
	} else {
		panic("Invalid migration file name")
		return nil
	}

	reDescription := regexp.MustCompile("[a-zA-Z]+")

	descMatches := reDescription.FindAllString(filename, 10)

	//remove the last bit i.e sql in file name
	descMatches = descMatches[:len(descMatches)-1]

	description := strings.Join(descMatches, " ")

	m := migration{
		Description: description,
		Timestamp:   timestamp,
		DoScript:    doScript,
		UndoScript:  undoScript,
	}

	SetMigrationStatus(&m)

	return &m
}

//Reads all migrations from files
func ReadMigrationsFromFile() migrations {
	fis, err := ioutil.ReadDir("./scripts/")
	if err != nil {
		panic(err)
	}

	var ms migrations
	for _, f := range fis {
		mig := ReadMigration(f.Name())
		ms = append(ms, *mig)
	}
	sort.Sort(ms)
	return ms
}

func main() {

	if len(os.Args) > 1 {
		command := os.Args[1]

		switch command {
		case "init":
			InitMigration()
		case "new":
			NewMigration()
		case "up":
			Up()
		case "down":
			Down()
		case "status":
			Status()
		default:
			log.Fatalln("Invalid command.")
		}
	} else {
		fmt.Println("usage: pgmigrate <command> <params>")
	}
}

//creates migration directory, config.js and initial migration
func InitMigration() {

	if len(os.Args) < 3 {
		log.Fatal("Missing parameters. Usage: pgmigrate init <path>")
		return
	}

	migrationPath := os.Args[2]

	migrationPath, err := filepath.Abs(migrationPath)
	fmt.Println("Initializing migrations at ", migrationPath)
	//confirm path exists
	stats, err := os.Stat(migrationPath)
	if err != nil {
		log.Fatalln(err)
		return
	}
	//confirm path is a directory
	if !stats.IsDir() {
		log.Fatalln("The migration path provided is not a directory")
		return
	}
	//confirm the directory is empty
	file, err := os.Open(migrationPath)
	if err != nil {
		log.Fatalln(err)
		return
	}
	_, err = file.Readdir(1)
	if err != io.EOF {
		log.Fatalln("migration directory is not empty ")
		return
	}
	//create pgmigrate.json
	var c config
	cbytes, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		log.Fatalln(err)
		return
	}
	err = ioutil.WriteFile(migrationPath+"/pgmigrate.json", cbytes, DEFAULT_FILE_PERMISSION)
	if err != nil {
		log.Fatalln(err)
		return
	}

	//create scripts folder
	err = os.Mkdir(migrationPath+"/scripts", DEFAULT_DIR_PERMISSION)
	if err != nil {
		log.Fatalln(err)
	}
}

//creates a new migration
func NewMigration() {
	//confirm description is provided
	if len(os.Args) < 3 {
		log.Fatalln("Invalid paramenters. Usage: pgmigrate new migration description text")
	}

	//create new migration
	var description string
	for i, s := range os.Args {
		if i > 1 {
			description = description + " " + s
		}
	}
	m := migration{Description: description, Timestamp: time.Now().Unix()}

	//write migration to file
	err := m.WriteToFile()
	if err != nil {
		log.Fatalln(err)
	}
}

func CreateChangeLogTable() {
	c := GetConfig()
	query := fmt.Sprintf("CREATE TABLE %s (id SERIAL PRIMARY KEY, timestamp NUMERIC, description VARCHAR(500));", c.MigrationTableName)
	db := getDb()
	db.Exec(query)
}

func Up() {

	CreateChangeLogTable()

	n := int64(0)
	if len(os.Args) > 2 {
		nstr := os.Args[2]
		var err error
		n, err = strconv.ParseInt(nstr, 10, 32)
		if err != nil {
			n = int64(0)
		}
	}
	migrations := ReadMigrationsFromFile()

	count := 0 //track number of migrations applied
	for _, m := range migrations {
		if !m.IsApplied {
			if n == int64(0) {
				log.Printf("Applying %s ...", m.Description)
				m.Do()
			} else {
				if int64(count) <= n {
					log.Printf("Applying %s ...", m.Description)
					m.Do()
					count++
				}
			}

		}
	}

}

func Down() {

	CreateChangeLogTable()

	n := int64(0)
	if len(os.Args) > 2 {
		nstr := os.Args[2]
		var err error
		n, err = strconv.ParseInt(nstr, 10, 32)
		if err != nil {
			n = int64(0)
		}
	}
	migrations := ReadMigrationsFromFile()

	count := 0
	for _, m := range migrations {
		if int64(count) <= n {
			if m.IsApplied {
				log.Printf("Undoing %s ...", m.Description)
				m.Undo()
				count++
			}
		}
	}
}

func Status() {
	migrations := ReadMigrationsFromFile()
	for _, m := range migrations {
		var status string
		if m.IsApplied {
			status = "Applied"
		} else {
			status = "Pending"
		}
		fmt.Printf("%d	%s		%s \n", m.Timestamp, m.Description, status)
	}
}
