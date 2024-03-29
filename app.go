package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
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

	_ "github.com/lib/pq"
)

const defaultFilePermission = 0644
const defaultDirPermission = 0755

//Config holds the migration config parameters
type Config struct {
	DbHost             string `json:"dbHost"`
	DbName             string `json:"dbName"`
	DbUsername         string `json:"dbUsername"`
	DbPassword         string `json:"dbPassword"`
	MigrationTableName string `json:"migrationTableName"`
}

//Migration encapsulates a migration
type Migration struct {
	Description string
	Timestamp   int64
	DoScript    string
	UndoScript  string
	IsApplied   bool
}

//Function encapsulates a function
type Function struct {
	Description    string
	Timestamp      int64
	FunctionScript string
}

//WriteToFile writes migration to file
func (m *Function) WriteToFile() error {
	tpl, err := template.New("FunctionTemplate").Parse(functionTpl)
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
	templPath := templAbsPath + "/scripts/functions/" + strconv.FormatInt(m.Timestamp, 10) + "_" + strings.Join(tempPathNames, "_") + ".sql"

	err = ioutil.WriteFile(templPath, templBytes, defaultFilePermission)
	if err != nil {
		return err
	}

	return nil
}

//Migrations is a slice of migrations
type Migrations []Migration

func (ms Migrations) Less(i, j int) bool {
	return ms[i].Timestamp < ms[j].Timestamp
}

func (ms Migrations) Swap(i, j int) {
	ms[i], ms[j] = ms[j], ms[i]
}

func (ms Migrations) Len() int {
	return len(ms)
}

//Functions
//Migrations is a slice of migrations
type Functions []Function

func (ms Functions) Less(i, j int) bool {
	return ms[i].Timestamp < ms[j].Timestamp
}

func (ms Functions) Swap(i, j int) {
	ms[i], ms[j] = ms[j], ms[i]
}

func (ms Functions) Len() int {
	return len(ms)
}

var migrationTpl = `-- {{.Description}} --
-- @DO sql script --


-- @UNDO sql script --


`

var functionTpl = `-- {{.Description}} --
drop function function_name;
create or replace function function_name returns return_type 
language plpgsql 
as $$
    declare 
        -- declarations
    begin 

    end;
$$;
`

//Do runs the function script
func (m *Function) RunFunction() {
	ExecuteSQL(m.FunctionScript)
}

//Do runs the do script
func (m *Migration) Do() {
	c := GetConfig()
	ExecuteSQL(m.DoScript)

	insertSQL := fmt.Sprintf("INSERT INTO %s (timestamp, description) VALUES ($1, $2)", c.MigrationTableName)
	db := getDb()
	_, err := db.Exec(insertSQL, m.Timestamp, m.Description)
	if err != nil {
		log.Fatalln(err)
	}
}

//Undo runs the undo script
func (m *Migration) Undo() {
	c := GetConfig()
	ExecuteSQL(m.UndoScript)

	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE timestamp = $1", c.MigrationTableName)
	_, err := db.Exec(deleteSQL, m.Timestamp)
	if err != nil {
		log.Fatalln(err)
	}
}

//WriteToFile writes migration to file
func (m *Migration) WriteToFile() error {
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

	err = ioutil.WriteFile(templPath, templBytes, defaultFilePermission)
	if err != nil {
		return err
	}

	return nil
}

var db *sql.DB

//MustReadConfig reads config file or exits in case of error
func MustReadConfig() *Config {
	configPath, err := filepath.Abs("./pgmigrate.json")
	if err != nil {
		log.Fatalln(err)
	}
	configBytes, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Fatalln(err)
	}
	var c Config
	json.Unmarshal(configBytes, &c)
	return &c
}

var conf *Config

//GetConfig gets the configuration, reads from file if the configuration was not already loaded
func GetConfig() *Config {
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
			log.Fatal(err)
		}
		db = newDb
	}
	return db
}

//ExecuteSQL executes a query without parameters
func ExecuteSQL(query string) {
	db := getDb()
	_, err := db.Exec(query)
	if err != nil {
		log.Fatalln(err)
		return
	}
}

//IsMigrationApplied checks if a migration is already applied
func IsMigrationApplied(m *Migration) bool {
	var count int
	conf := GetConfig()
	db := getDb()
	err := db.QueryRow("SELECT COUNT(*) as count FROM "+conf.MigrationTableName+" WHERE timestamp = $1", m.Timestamp).Scan(&count)
	if err != nil {
		log.Fatalln(err)
	}

	if count > 0 {
		return true
	}
	return false
}

//SetMigrationStatus marks migration as either applied or not
func SetMigrationStatus(m *Migration) {
	if IsMigrationApplied(m) {
		m.IsApplied = true
	}
}

//ReadMigration reads a migration from file
func ReadMigration(filename string) *Migration {
	migrationBytes, err := ioutil.ReadFile("./scripts/" + filename)
	if err != nil {
		log.Fatalln(err)
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
			log.Fatalln(err)
		}
	} else {
		log.Fatalln("Invalid migration file name")
	}

	reDescription := regexp.MustCompile("[a-zA-Z]+")

	descMatches := reDescription.FindAllString(filename, 10)

	//remove the last bit i.e sql in file name
	descMatches = descMatches[:len(descMatches)-1]

	description := strings.Join(descMatches, " ")

	m := Migration{
		Description: description,
		Timestamp:   timestamp,
		DoScript:    doScript,
		UndoScript:  undoScript,
	}

	SetMigrationStatus(&m)

	return &m
}

//ReadFunction reads a function from file
func ReadFunction(filename string) *Function {
	functionBytes, err := ioutil.ReadFile("./scripts/functions/" + filename)
	if err != nil {
		log.Fatalln(err)
	}
	functionScript := string(functionBytes)

	//get the timestamp part
	re := regexp.MustCompile("[0-9]+")
	matches := re.FindAllString(filename, 1)

	var timestamp int64
	if len(matches) > 0 {
		timestamp, err = strconv.ParseInt(matches[0], 10, 64)
		if err != nil {
			log.Fatalln(err)
		}
	} else {
		log.Fatalln("Invalid function file name")
	}

	reDescription := regexp.MustCompile("[a-zA-Z]+")

	descMatches := reDescription.FindAllString(filename, 10)

	//remove the last bit i.e sql in file name
	descMatches = descMatches[:len(descMatches)-1]

	description := strings.Join(descMatches, " ")

	f := Function{
		Description:    description,
		Timestamp:      timestamp,
		FunctionScript: functionScript,
	}

	return &f
}

func ReadFunctionsFromFile() Functions {
	fis, err := ioutil.ReadDir("./scripts/functions/")
	if err != nil {
		log.Fatalln(err)
	}

	var ms Functions
	for _, f := range fis {
		mig := ReadFunction(f.Name())
		ms = append(ms, *mig)
	}
	sort.Sort(ms)
	return ms
}

//ReadMigrationsFromFile reads all migrations from files
func ReadMigrationsFromFile() Migrations {
	fis, err := ioutil.ReadDir("./scripts/")
	if err != nil {
		log.Fatalln(err)
	}

	var ms Migrations
	for _, f := range fis {
		if !f.IsDir() {
			mig := ReadMigration(f.Name())
			ms = append(ms, *mig)
		}
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
		case "function":
			NewFunction()
		case "run-functions":
			RunFunctions()
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

//InitMigration creates migration directory, config.js and initial migration
func InitMigration() {

	if len(os.Args) < 2 {
		log.Fatalln("Missing parameters. Usage: pgmigrate init <path>")
	}

	migrationPath := "."
	if len(os.Args) > 2 {
		migrationPath = os.Args[2]
	}

	migrationPath, err := filepath.Abs(migrationPath)
	if err != nil {
		log.Fatalln("Unable to get absolute path: ", err)
	}
	fmt.Println("Initializing migrations at: ", migrationPath)
	//confirm path exists
	stats, err := os.Stat(migrationPath)
	if err != nil {
		log.Fatalln(err)
	}
	//confirm path is a directory
	if !stats.IsDir() {
		log.Fatalln("The migration path provided is not a directory")
	}
	//confirm the directory is empty
	file, err := os.Open(migrationPath)
	if err != nil {
		log.Fatalln(err)
	}
	_, err = file.Readdir(1)
	if err != io.EOF {
		log.Fatalln("migration directory is not empty ")
	}
	//create pgmigrate.json
	var c Config
	cbytes, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		log.Fatalln(err)
	}
	err = ioutil.WriteFile(migrationPath+"/pgmigrate.json", cbytes, defaultFilePermission)
	if err != nil {
		log.Fatalln(err)
	}

	//create scripts folder
	err = os.Mkdir(migrationPath+"/scripts", defaultDirPermission)
	if err != nil {
		log.Fatalln(err)
	}

	//make functions folder
	err = os.Mkdir(migrationPath+"/scripts/functions", defaultDirPermission)
	if err != nil {
		log.Fatalln(err)
	}
}

//NewMigration creates a new migration
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
	m := Migration{Description: description, Timestamp: time.Now().Unix()}

	//write migration to file
	err := m.WriteToFile()
	if err != nil {
		log.Fatalln(err)
	}
}

//NewFunction creates a new function
func NewFunction() {
	//confirm description is provided
	if len(os.Args) < 3 {
		log.Fatalln("Invalid paramenters. Usage: pgmigrate function migration description text")
	}

	//create new migration
	var description string
	for i, s := range os.Args {
		if i > 1 {
			description = description + " " + s
		}
	}
	m := Function{Description: description, Timestamp: time.Now().Unix()}

	//write migration to file
	err := m.WriteToFile()
	if err != nil {
		log.Fatalln(err)
	}
}

//CreateChangeLogTable creates changelog table
func CreateChangeLogTable() {
	c := GetConfig()
	query := fmt.Sprintf("CREATE TABLE %s (id SERIAL PRIMARY KEY, timestamp NUMERIC, description VARCHAR(500));", c.MigrationTableName)
	db := getDb()
	db.Exec(query)
}

//Up applies the 'up' migration
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

//Down applies the 'down' migration
func Down() {

	CreateChangeLogTable()

	n := int64(0)
	if len(os.Args) > 2 {
		nstr := os.Args[2]
		var err error
		n, err = strconv.ParseInt(nstr, 10, 64)
		if err != nil {
			n = int64(0)
		}
	}
	migrations := ReadMigrationsFromFile()
	//reverse the order of migrations when going down
	sort.Sort(sort.Reverse(migrations))
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

//Down applies the 'down' migration
func RunFunctions() {

	functions := ReadFunctionsFromFile()
	//reverse the order of migrations when going down
	sort.Sort(sort.Reverse(functions))
	for _, f := range functions {
		f.RunFunction()
	}
}

//Status shows the status of all migrations
func Status() {
	CreateChangeLogTable()
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
