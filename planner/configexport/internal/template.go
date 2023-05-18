package internal

const (
	generateConfigTemplate = `

// {{.Name}} {{.DisplayName}}
type {{.Name}} struct {
	{{range $index, $value := .Fields}}{{if eq $value.Server true}}{{if eq $value.Ignore false}}{{$value.Name}} {{$value.Type}} // {{$value.Describe}}{{end}}{{end}}
	{{end}}
} 

{{range $index, $value := .Fields}}{{$value}}{{end}}
`

	generateGoStructTemplate = `
{{if eq .Ignore false}}
type {{.TypeNotStar}} struct {
	{{range $index, $value := .Fields}}
		{{$value.Name}} {{$value.Type}}
	{{end}}
} 
{{end}}
`

	GenerateGoConfigTemplate = `// Code generated by minotaur-config-export. DO NOT EDIT.

package {{.Package}}

import (
	jsonIter "github.com/json-iterator/go"
	"os"
)

var json = jsonIter.ConfigCompatibleWithStandardLibrary

var (
{{range $index, $config := .Configs}}
	 Game{{$config.Name}} {{$config.GetVariable}}
	 game{{$config.Name}} {{$config.GetVariable}}
{{end}}
)

func LoadConfig(handle func(filename string, config any) error) {
{{range $index, $config := .Configs}}
	handle("{{$config.Prefix}}{{$config.Name}}.json", &game{{$config.Name}})
{{end}}
}

func Refresh() {
{{range $index, $config := .Configs}}
	Game{{$config.Name}} = game{{$config.Name}}
{{end}}
}


func DefaultLoad(filepath string) {
	LoadConfig(func(filename string, config any) error {
		bytes, err := os.ReadFile(filepath)
		if err != nil {
			return err
		}
		return json.Unmarshal(bytes, &config)
	})
}
`

	GenerateGoDefineTemplate = `// Code generated by minotaur-config-export. DO NOT EDIT.

package {{.Package}}

{{range $index, $config := .Configs}}
	 {{$config.String}}
{{end}}

`
)
