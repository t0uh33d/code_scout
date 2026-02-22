package utils

const (
	COOKIE_INFORMATION_ERR = "Could not get the information from the cookie: "
	JWT_INFORMATION_ERR    = "Could not get the information from the token."
	NO_AUTHORIZED_USER_ERR = "No authorized user. Cookie expired or not exists, please, login again."
)

func ParameterNotExistsErr(param string) string {
	return "the parameter " + param + " does not exist"
}

func ParameterNotUuidErr(param string) string {
	return "the parameter " + param + " is not Uuid"
}
