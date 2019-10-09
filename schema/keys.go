package schema

import "fmt"

// PrimaryKey represents a primary key in a database
type PrimaryKey struct {
	Columns []string
}

// Index represents an index in a database
type Index struct {
	Name    string
	Columns []string
}

// Unique represents a unique constraint in a database
type Unique struct {
	Name    string
	Columns []string
}

// ForeignKey represents a foreign key constraint in a database
type ForeignKey struct {
	Name string

	LocalColumns   []string
	ForeignModel   string
	ForeignColumns []string
}

// SQLColumnDef formats a field name and type like an SQL field definition.
type SQLColumnDef struct {
	Name string
	Type GoType
}

// String for fmt.Stringer
func (s SQLColumnDef) String() string {
	return fmt.Sprintf("%s %s", s.Name, s.Type)
}

// SQLColumnDefs has small helper functions
type SQLColumnDefs []SQLColumnDef

// Names returns all the names
func (s SQLColumnDefs) Names() []string {
	names := make([]string, len(s))

	for i, sqlDef := range s {
		names[i] = sqlDef.Name
	}

	return names
}

// Types returns all the types
func (s SQLColumnDefs) Types() []GoType {
	types := make([]GoType, len(s))

	for i, sqlDef := range s {
		types[i] = sqlDef.Type
	}

	return types
}

// SQLColDefinitions creates a definition in sql format for a field
func SQLColDefinitions(cols []*Column, names []string) SQLColumnDefs {
	ret := make([]SQLColumnDef, len(names))

	for i, n := range names {
		for _, c := range cols {
			if n != c.Name {
				continue
			}

			ret[i] = SQLColumnDef{Name: n, Type: c.Type.GoType()}
		}
	}

	return ret
}
