package error_codes

const (
	AUTHORIZATION_HEADER_MISSING_ERR = "Missing Authorization header"
	UNAUTHORIZED_ERR                 = "Unauthorized"
	INVALID_REQUEST_DATA_ERR         = "Invalid request data"

	ERR_INVALID_PROJECT_NAME_ERR      = "Project name cannot be empty"
	ERR_FAILED_TO_CREATE_PROJECT_ERR  = "Failed to create project"
	ERR_FAILED_TO_DELETE_PROJECT_ERR  = "Failed to delete project"
	PROJECT_ID_HEADER_MISSING_ERR     = "Missing Project ID header"
	PROJECT_SECRET_HEADER_MISSING_ERR = "Missing Project Secret header"
	INVALID_PROJECT_ID_HEADER_ERR     = "Invalid Project ID header"
	INVALID_PROJECT_SECRET_HEADER_ERR = "Invalid Project Secret header"
)
