package utils

import (
	"github.com/go-resty/resty"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"errors"
	"net/http"
)

// Returns the AccessToken, APIManagerEndpoint, Errors given an Environment
// Deals with generating tokens needed for executing a particular command
func ExecutePreCommand(environment string, flagUsername string, flagPassword string) (string, string, error) {
	if EnvExistsInEndpointsFile(environment) {
		registrationEndpoint := GetRegistrationEndpointOfEnv(environment)
		apiManagerEndpoint := GetAPIMEndpointOfEnv(environment)
		tokenEndpoint := GetTokenEndpointOfEnv(environment)
		var username string
		var password string
		var clientID string
		var clientSecret string
		var err error

		if EnvExistsInKeysFile(environment) {
			// client_id, client_secret, and username exist in file
			username = GetUsernameOfEnv(environment)

			if flagUsername != "" {
				// flagUsername is not blank
				if flagUsername != username {
					// username entered with flag -u is not the same as username found in env_keys_all.yaml file
					Logln(LogPrefixWarning + "Username entered with flag -u for the environment '" + environment + "' is not the same as username found in env_keys_all.yaml file")
					fmt.Println("Username entered is not found under '" + environment + "' in env_keys_all.yaml file")
					//log.Println("Execute 'wso2apim reset-user -e " + environment +"' to clear user data")
					fmt.Println("Execute 'wso2apim reset-user -e " + environment + "' to clear user data")
					os.Exit(1)
				} else {
					// username entered with flag -u is the same as username found in env_keys_all.yaml file
					if flagPassword == "" {
						fmt.Println("For Username: " + username)
						password = PromptForPassword()
					} else {
						// flagPassword is not blank
						// no need of prompting for password now
						password = flagPassword
					}
				}
			} else {
				// flagUsername is blank
				if flagPassword != "" {
					// flagPassword is not blank
					password = flagPassword
				} else {
					// flagPassword is blank
					fmt.Println("For username: " + username)
					password = PromptForPassword()
				}
			}

			clientID = GetClientIDOfEnv(environment)
			clientSecret = GetClientSecretOfEnv(environment, password)

			Logln(LogPrefixInfo+"Username:", username)
			Logln(LogPrefixInfo+"ClientID:", clientID)
		} else {
			// env exists in endpoints file, but not in keys file
			// no client_id, client_secret in file
			// first use of the environment
			// Get new values

			username, password := HandleFlagsUsernamePasswordWithEnvInEndpointsFile(flagUsername, flagPassword)

			fmt.Println("\nUsername: " + username + "\n")
			clientID, clientSecret, err = GetClientIDSecret(username, password, registrationEndpoint)

			if err != nil {
				fmt.Println("Error:", err)
			}

			// Persist clientID, clientSecret, Username in file
			encryptedClientSecret := Encrypt([]byte(GetMD5Hash(password)), clientSecret)
			envKeys := EnvKeys{clientID, encryptedClientSecret, username}
			AddNewEnvToKeysFile(environment, envKeys)
		}

		// Get OAuth Tokens
		m, _ := GetOAuthTokens(username, password, GetBase64EncodedCredentials(clientID, clientSecret), tokenEndpoint)
		accessToken := m["access_token"]

		Logln(LogPrefixInfo+"AccessToken:", accessToken)

		return accessToken, apiManagerEndpoint, nil
	} else {
		return "", "", errors.New("Details incorrect/unavailable for environment '" + environment + "' in env_endpoints_all.yaml")
	}
}

// Method for

func HandleFlagsUsernamePasswordWithEnvInEndpointsFile(flagUsername string, flagPassword string) (username, password string) {
	if flagUsername != "" {
		// flagUsername is not blank
		username = flagUsername
		if flagPassword == "" {
			// flagPassword is blank
			fmt.Println("For Username: " + username)
			password = PromptForPassword()
		} else {
			// flagPassword is not blank
			password = flagPassword
		}
	} else {
		// flagUsername is blank
		// doesn't matter is flagPassword is blank or not
		username = strings.TrimSpace(PromptForUsername())
		password = PromptForPassword()
	}

	return username, password
}

// GetClientIDSecret implemented using go-resty
// provide username, password
// returns client_id, client_secret
func GetClientIDSecret(username string, password string, url string) (string, string, error) {
	body := `{"clientName": "Test", "redirect_uris": "www.google.lk", "grant_types":"password"}`
	headers := make(map[string]string)

	headers[HeaderContentType] = HeaderValueApplicationJSON
	// headers["Content-Type"] = "application/json"

	//headers["Accept"] = HeaderValueApplicationJSON

	headers[HeaderAuthorization] = HeaderValueAuthBasicPrefix + " " + GetBase64EncodedCredentials(username, password)
	// headers["Authorization"] = "Basic " + GetBase64EncodedCredentials(username, password)

	resty.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) // To bypass errors in HTTPS certificates

	// POST request using resty
	resp, err := InvokePOSTRequest(url, headers, body)

	if err != nil {
		fmt.Println("Error in connecting:")
		fmt.Println(err)
		os.Exit(1)
	}

	if resp.StatusCode() != http.StatusOK {
		if resp.StatusCode() == http.StatusUnauthorized {
			HandleErrorAndExit("Incorrect Username/Password combination", errors.New("401 Unauthorized"))
		}
		return "", "", errors.New("Request didn't respond 200 OK: " + resp.Status())
	}

	m := make(map[string]string) // a map to hold response data
	data := []byte(resp.Body())
	err = json.Unmarshal(data, &m) // add response data to m

	clientID := m["client_id"]
	clientSecret := m["client_secret"]

	return clientID, clientSecret, err
}

// Encode the concatenation of two strings (using ":")
// provide two strings
// returns base64Encode(key:secret)
func GetBase64EncodedCredentials(key string, secret string) string {
	line := key + ":" + secret
	encoded := base64.StdEncoding.EncodeToString([]byte(line))
	return encoded
}

// GetOAuthTokens implemented using go-resty/resty
// provide username, password, and validity period for the access token
// returns the response as a map
func GetOAuthTokens(username string, password string, b64EncodedClientIDClientSecret string, url string) (map[string]string, error) {
	validityPeriod := DefaultTokenValidityPeriod
	body := "grant_type=password&username=" + username + "&password=" + password + "&validity_period=" + validityPeriod

	// set headers
	headers := make(map[string]string)
	headers[HeaderContentType] = HeaderValueXWWWFormUrlEncoded
	headers[HeaderAuthorization] = HeaderValueAuthBearerPrefix + " " + b64EncodedClientIDClientSecret
	headers[HeaderAccept] = HeaderValueApplicationJSON

	resty.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) // To bypass errors in HTTP certificates
	resp, err := InvokePOSTRequest(url, headers, body)

	if err != nil {
		Logln(LogPrefixError + "connecting to " + url)
		HandleErrorAndExit("Unable to Connect", err)
	}

	if resp.StatusCode() != http.StatusOK {
		HandleErrorAndExit("Unable to connect", errors.New("Status: "+resp.Status()))
		return nil, nil
	}

	m := make(map[string]string) // a map to hold response data
	data := []byte(resp.Body())
	_ = json.Unmarshal(data, &m) // add response data to m

	return m, nil // m contains 'access_token', 'refresh_token' etc
}
