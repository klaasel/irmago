package cmd

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/go-errors/errors"
	"github.com/privacybydesign/irmago/server"
	"github.com/privacybydesign/irmago/server/requestorserver"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/x-cray/logrus-prefixed-formatter"
)

var logger = logrus.StandardLogger()
var conf *requestorserver.Configuration

var RootCommand = &cobra.Command{
	Use:   "irmad",
	Short: "IRMA server for verifying and issuing attributes",
	Run: func(command *cobra.Command, args []string) {
		if err := configure(command); err != nil {
			die(errors.WrapPrefix(err, "Failed to read configuration", 0))
		}
		serv, err := requestorserver.New(conf)
		if err != nil {
			die(errors.WrapPrefix(err, "Failed to configure server", 0))
		}
		if err := serv.Start(conf); err != nil {
			die(errors.WrapPrefix(err, "Failed to start server", 0))
		}
	},
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the RootCommand.
func Execute() {
	logger.Level = logrus.InfoLevel
	logger.SetFormatter(&prefixed.TextFormatter{
		FullTimestamp: true,
	})
	if err := setFlags(RootCommand); err != nil {
		die(errors.WrapPrefix(err, "Failed to attach flags to "+RootCommand.Name()+" command", 0))
	}
	if err := RootCommand.Execute(); err != nil {
		die(errors.Wrap(err, 0))
	}
}

func die(err *errors.Error) {
	msg := err.Error()
	if logger.IsLevelEnabled(logrus.DebugLevel) {
		msg += "\nStack trace:\n" + string(err.Stack())
	}
	logger.Fatal(msg)
}

func setFlags(cmd *cobra.Command) error {
	flags := cmd.Flags()
	flags.SortFlags = false

	schemespath := server.DefaultSchemesPath()
	defaulturl, err := server.LocalIP()
	if err != nil {
		logger.Warn("Could not determine local IP address: ", err.Error())
	} else {
		defaulturl = "http://" + defaulturl + ":port"
	}

	flags.StringP("config", "c", "", "path to configuration file")
	flags.StringP("schemes-path", "i", schemespath, "path to irma_configuration")
	flags.String("schemes-assets-path", "", "if specified, copy schemes from here into schemes-path")
	flags.Int("schemes-update", 60, "update IRMA schemes every x minutes (0 to disable)")
	flags.Int("max-request-age", 300, "max age in seconds of a session request JWT")
	flags.StringP("url", "u", defaulturl, "external URL to server to which the IRMA client connects")

	flags.IntP("port", "p", 8088, "port at which to listen")
	flags.StringP("listen-addr", "l", "", "address at which to listen (default 0.0.0.0)")
	flags.Int("client-port", 0, "if specified, start a separate server for the IRMA app at this port")
	flags.String("client-listen-addr", "", "address at which server for IRMA app listens")
	flags.Lookup("port").Header = `Server address and port to listen on. If the client* configuration options are provided (see also the TLS flags)
then the endpoints at /session for the requestor and /irma for the irmaclient (i.e. IRMA app) will listen on
distinct network endpoints (e.g., localhost:1234/session and 0.0.0.0:5678/irma).`

	flags.Bool("no-auth", true, "whether or not to authenticate requestors")
	flags.String("requestors", "", "requestor configuration (in JSON)")
	flags.Lookup("no-auth").Header = `Requestor authentication. If disabled, then anyone that can reach this server can submit requests to it.
If it is enabled, then requestor specific configuration must be provided.`

	flags.StringSlice("disclose-perms", nil, "list of attributes that all requestors may verify (default *)")
	flags.StringSlice("sign-perms", nil, "list of attributes that all requestors may request in signatures (default *)")
	flags.StringSlice("issue-perms", nil, "list of attributes that all requestors may issue")
	flags.Lookup("disclose-perms").Header = `Default requestor permissions. Apply to all requestors, in addition to requestor specific permissions.
May contain wildcards. Separate multiple with comma. Example: irma-demo.*,pbdf.*. By default all requestors
may use all attributes in disclosure and signature sessions. Pass empty string to disable session type.`

	flags.StringP("privkeys", "k", "", "path to IRMA private keys")
	flags.Lookup("privkeys").Header = `Path to a folder containing IRMA private keys, with filenames scheme.issuer.xml, e.g. irma-demo.MijnOverheid.xml.
Private keys may also be stored in the scheme (e.g. irma-demo/MijnOverheid/PrivateKeys/0.xml).`

	flags.StringP("jwt-issuer", "j", "irmaserver", "JWT issuer")
	flags.String("jwt-privkey", "", "JWT private key")
	flags.String("jwt-privkeyfile", "", "path to JWT private key")
	flags.Lookup("jwt-issuer").Header = `JWT configuration. Can be omitted but then endpoints that return signed JWTs are disabled.
All of the keys and certificates below are expected in PEM. Pass it either directly, or a path to it
using the corresponding "-file" flag.`

	flags.String("tls-cert", "", "TLS certificate (chain)")
	flags.String("tls-cert-file", "", "path to TLS certificate (chain)")
	flags.String("tls-privkey", "", "TLS private key")
	flags.String("tls-privkey-file", "", "path to TLS private key")
	flags.String("client-tls-cert", "", "TLS certificate (chain) for IRMA app server")
	flags.String("client-tls-cert-file", "", "path to TLS certificate (chain) for IRMA app server")
	flags.String("client-tls-privkey", "", "TLS private key for IRMA app server")
	flags.String("client-tls-privkey-file", "", "path to TLS private key for IRMA app server")
	flags.Lookup("tls-cert").Header = "TLS configuration. Leave empty to disable TLS."

	flags.CountP("verbose", "v", "verbose (repeatable)")
	flags.BoolP("quiet", "q", false, "quiet")
	flags.Bool("log-json", false, "Log in JSON format")
	flags.Bool("production", false, "Production mode")
	flags.Lookup("verbose").Header = `Other options`

	return nil
}

func configure(cmd *cobra.Command) error {
	dashReplacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(dashReplacer)
	viper.SetFileKeyReplacer(dashReplacer)
	viper.SetEnvPrefix("IRMASERVER")
	viper.AutomaticEnv()
	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		return err
	}

	if viper.GetBool("production") {
		viper.SetDefault("no-auth", false)
		viper.SetDefault("url", "")
	}

	// Locate and read configuration file
	confpath := viper.GetString("config")
	if confpath != "" {
		dir, file := filepath.Dir(confpath), filepath.Base(confpath)
		viper.SetConfigName(strings.TrimSuffix(file, filepath.Ext(file)))
		viper.AddConfigPath(dir)
	} else {
		viper.SetConfigName("irmaserver")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/irmaserver/")
		viper.AddConfigPath("$HOME/.irmaserver")
	}
	err := viper.ReadInConfig() // Hold error checking until we know how much of it to log

	// Set log level
	if viper.GetBool("log-json") {
		logger.SetFormatter(&logrus.JSONFormatter{})
	}
	logger.Level = server.Verbosity(viper.GetInt("verbose"))
	if viper.GetBool("quiet") {
		logger.Out = ioutil.Discard
	}

	logger.Debug("Configuring")
	logger.Debug("Log level: ", logger.Level.String())
	if err != nil {
		if _, notfound := err.(viper.ConfigFileNotFoundError); notfound {
			logger.Info("No configuration file found")
		} else {
			die(errors.WrapPrefix(err, "Failed to unmarshal configuration file at "+viper.ConfigFileUsed(), 0))
		}
	} else {
		logger.Info("Config file: ", viper.ConfigFileUsed())
	}

	// Read configuration from flags and/or environmental variables
	conf = &requestorserver.Configuration{
		Configuration: &server.Configuration{
			DownloadDefaultSchemes: true, // If we get passed an empty schemes-path, download default schemes into it
			SchemesPath:            viper.GetString("schemes-path"),
			SchemesAssetsPath:      viper.GetString("schemes-assets-path"),
			SchemeUpdateInterval:   viper.GetInt("schemes-update"),
			IssuerPrivateKeysPath:  viper.GetString("privkeys"),
			URL:    viper.GetString("url"),
			Logger: logger,
		},
		Permissions: requestorserver.Permissions{
			Disclosing: handlePermission("disclose-perms"),
			Signing:    handlePermission("sign-perms"),
			Issuing:    handlePermission("issue-perms"),
		},
		ListenAddress:                  viper.GetString("listen-addr"),
		Port:                           viper.GetInt("port"),
		ClientListenAddress:            viper.GetString("client-listen-addr"),
		ClientPort:                     viper.GetInt("client-port"),
		DisableRequestorAuthentication: viper.GetBool("no-auth"),
		Requestors:                     make(map[string]requestorserver.Requestor),
		JwtIssuer:                      viper.GetString("jwt-issuer"),
		JwtPrivateKey:                  viper.GetString("jwt-privkey"),
		JwtPrivateKeyFile:              viper.GetString("jwt-privkey-file"),
		MaxRequestAge:                  viper.GetInt("max-request-age"),
		Verbose:                        viper.GetInt("verbose"),
		Quiet:                          viper.GetBool("quiet"),
		LogJSON:                        viper.GetBool("log-json"),

		TlsCertificate:           viper.GetString("tls-cert"),
		TlsCertificateFile:       viper.GetString("tls-cert-file"),
		TlsPrivateKey:            viper.GetString("tls-privkey"),
		TlsPrivateKeyFile:        viper.GetString("tls-privkey-file"),
		ClientTlsCertificate:     viper.GetString("client-tls-cert"),
		ClientTlsCertificateFile: viper.GetString("client-tls-cert-file"),
		ClientTlsPrivateKey:      viper.GetString("client-tls-privkey"),
		ClientTlsPrivateKeyFile:  viper.GetString("client-tls-privkey-file"),

		Production: viper.GetBool("production"),
	}

	// Handle requestors
	if len(viper.GetStringMap("requestors")) > 0 { // First read config file
		if err := viper.UnmarshalKey("requestors", &conf.Requestors); err != nil {
			return errors.WrapPrefix(err, "Failed to unmarshal requestors from config file", 0)
		}
	}
	requestors := viper.GetString("requestors") // Read flag or env var
	if len(requestors) > 0 {
		if err := json.Unmarshal([]byte(requestors), &conf.Requestors); err != nil {
			return errors.WrapPrefix(err, "Failed to unmarshal requestors from json", 0)
		}
	}

	logger.Debug("Done configuring")

	return nil
}

func handlePermission(typ string) []string {
	if !viper.IsSet(typ) && (!viper.GetBool("production") || typ != "issue-perms") {
		return []string{"*"}
	}
	perms := viper.GetStringSlice(typ)
	if perms == nil {
		return []string{}
	}
	return perms
}