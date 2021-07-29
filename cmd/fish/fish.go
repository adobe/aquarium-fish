package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	dqlite_app "github.com/canonical/go-dqlite/app"
	dqlite_client "github.com/canonical/go-dqlite/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"git.corp.adobe.com/CI/aquarium-fish/lib/crypt"
	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
	"git.corp.adobe.com/CI/aquarium-fish/lib/openapi"
)

func main() {
	var api_address string
	var db_address string
	var db_join *[]string
	var cfg_path string
	var dir string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "aquarium-fish",
		Short: "Aquarium fish",
		Long:  `Part of the Aquarium suite - a distributed resources manager`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("Fish running...")

			cfg := &fish.Config{}
			if err := cfg.ReadConfigFile(cfg_path); err != nil {
				log.Println("Fish: Unable to apply config file:", cfg_path, err)
				return err
			}
			if api_address != "" {
				cfg.APIAddress = api_address
			}
			if db_address != "" {
				cfg.DBAddress = db_address
			}
			if len(*db_join) > 0 {
				cfg.DBJoin = *db_join
			}
			if dir != "" {
				cfg.Directory = dir
			}

			dir := filepath.Join(cfg.Directory, cfg.DBAddress)
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return errors.Wrapf(err, "can't create %s", dir)
			}
			logFunc := func(l dqlite_client.LogLevel, format string, a ...interface{}) {
				if !verbose {
					return
				}
				log.Printf(fmt.Sprintf("%s: %s: %s\n", cfg.APIAddress, l.String(), format), a...)
			}

			log.Println("Fish init TLS...")
			ca_path := cfg.TLSCaCrt
			if !filepath.IsAbs(ca_path) {
				ca_path = filepath.Join(cfg.Directory, ca_path)
			}
			key_path := cfg.TLSKey
			if !filepath.IsAbs(key_path) {
				key_path = filepath.Join(cfg.Directory, key_path)
			}
			cert_path := cfg.TLSCrt
			if !filepath.IsAbs(cert_path) {
				cert_path = filepath.Join(cfg.Directory, cert_path)
			}
			if err := crypt.InitTlsPairCa([]string{cfg.NodeName, cfg.DBAddress}, ca_path, key_path, cert_path); err != nil {
				return err
			}
			tls_pair, err := tls.LoadX509KeyPair(cert_path, key_path)
			if err != nil {
				return err
			}
			tls_ca, err := ioutil.ReadFile(ca_path)
			if err != nil {
				return err
			}
			ca_pool := x509.NewCertPool()
			ca_pool.AppendCertsFromPEM(tls_ca)
			log.Println(fmt.Sprintf("  loaded ca:%s key:%s crt:%s", ca_path, key_path, cert_path))

			listen_cfg := makeTlsConfig(tls_pair, ca_pool, true)
			dial_cfg := makeTlsConfig(tls_pair, ca_pool, false)

			log.Println("Fish starting dqlite...")
			dqlite, err := dqlite_app.New(dir,
				dqlite_app.WithAddress(cfg.DBAddress),
				dqlite_app.WithCluster(cfg.DBJoin),
				dqlite_app.WithLogFunc(logFunc),
				dqlite_app.WithTLS(listen_cfg, dial_cfg),
			)
			if err != nil {
				return err
			}

			if err := dqlite.Ready(context.Background()); err != nil {
				return err
			}

			dqlite_db, err := dqlite.Open(context.Background(), "aquarium-fish")
			if err != nil {
				return err
			}

			log.Println("Fish starting ORM...")
			db, err := gorm.Open(&sqlite.Dialector{Conn: dqlite_db}, &gorm.Config{
				Logger: logger.Default.LogMode(logger.Warn),
			})
			if err != nil {
				return err
			}

			log.Println("Fish starting server...")
			fish, err := fish.New(db, cfg)
			if err != nil {
				return err
			}
			srv, err := openapi.Init(fish, cfg.APIAddress, cert_path, key_path)
			if err != nil {
				return err
			}

			log.Println("Fish initialized")
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, unix.SIGINT)
			signal.Notify(quit, unix.SIGQUIT)
			signal.Notify(quit, unix.SIGTERM)

			<-quit

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				log.Fatal("Fish forced to shutdown:", err)
			}

			fish.Close()

			log.Println("Fish exiting...")
			dqlite_db.Close()

			dqlite.Handover(context.Background())
			dqlite.Close()

			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&api_address, "api", "a", "", "address used to expose the fish API")
	flags.StringVarP(&db_address, "db", "d", "", "address used for internal database replication")
	db_join = flags.StringSliceP("db_join", "j", nil, "database addresses of existing nodes, comma separated")
	flags.StringVarP(&cfg_path, "cfg", "c", "", "yaml configuration file")
	flags.StringVarP(&dir, "dir", "D", "", "database and other fish files directory")
	flags.BoolVarP(&verbose, "verbose", "v", false, "verbose logging")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func makeTlsConfig(cert tls.Certificate, pool *x509.CertPool, server bool) *tls.Config {
	// Replace for the dqlite SimpleTLSConfig to not set ServerName
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		},
		PreferServerCipherSuites: true,
		RootCAs:                  pool,
		Certificates:             []tls.Certificate{cert},
	}
	if server {
		cfg.CurvePreferences = []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg
}
