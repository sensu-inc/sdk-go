module github.com/sensu-inc/sdk-go/integrations/openai

go 1.24

replace github.com/sensu-inc/sdk-go => ../../

require (
	github.com/google/uuid v1.6.0
	github.com/openai/openai-go v1.12.0
	github.com/sensu-inc/sdk-go v0.0.0-00010101000000-000000000000
)

require (
	github.com/tidwall/gjson v1.14.4 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
)
