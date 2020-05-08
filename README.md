pgmigrate
=========


Database migration tool for postgres written in go. 
This tool is inspired by mybatis migrations.

```
Usage: pgmigrate command [parameter]

Commands:
  init               Creates (if necessary) and initializes a migration path.
  new <description>  Creates a new migration with the provided description.
  up [n]             Run unapplied migrations, ALL by default, or 'n' specified.
  down [n]           Undoes migrations applied to the database. ONE by default or 'n' specified.
  status             Prints the changelog from the database if the changelog table exists `
```
