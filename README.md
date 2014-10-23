pgmigrate
=========

Database migration tool for postgres written in go. 

Usage: migrate command [parameter] [--path=<directory>] [--env=<environment>] [--template=<path to custom template>]

--path=<directory>   Path to repository.  Default current working directory.
--env=<environment>  Environment to configure. Default environment is 'development'.
--template=<template>  Path to custom template for creating new sql scripts.
--force              Forces script to continue even if SQL errors are encountered.
--help               Displays this usage message.
--trace              Shows additional error details (if any).

Commands:
  info               Display build version informations.
  init               Creates (if necessary) and initializes a migration path.
  bootstrap          Runs the bootstrap SQL script (see scripts/bootstrap.sql for more).
  new <description>  Creates a new migration with the provided description.
  up [n]             Run unapplied migrations, ALL by default, or 'n' specified.
  down [n]           Undoes migrations applied to the database. ONE by default or 'n' specified.
  version <version>  Migrates the database up or down to the specified version.
  status             Prints the changelog from the database if the changelog table exists.



