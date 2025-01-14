package cmd

import (
	"fmt"

	"github.com/inconshreveable/log15"
	"github.com/knqyf263/gost/db"
	"github.com/knqyf263/gost/fetcher"
	"github.com/knqyf263/gost/models"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/xerrors"
)

// redHatAPICmd represents the redhatAPI command
var redHatAPICmd = &cobra.Command{
	Use:   "redhatapi",
	Short: "Fetch the CVE information from Red Hat API",
	Long:  `Fetch the CVE information from Red Hat API`,
	RunE:  fetchRedHatAPI,
}

func init() {
	fetchCmd.AddCommand(redHatAPICmd)

	redHatAPICmd.PersistentFlags().String("after", "1970-01-01", "Fetch CVEs after the specified date (e.g. 2017-01-01)")
	_ = viper.BindPFlag("after", redHatAPICmd.PersistentFlags().Lookup("after"))

	redHatAPICmd.PersistentFlags().String("before", "", "Fetch CVEs before the specified date (e.g. 2017-01-01)")
	_ = viper.BindPFlag("before", redHatAPICmd.PersistentFlags().Lookup("before"))

	redHatAPICmd.PersistentFlags().Bool("list-only", false, "")
	_ = viper.BindPFlag("list-only", redHatAPICmd.PersistentFlags().Lookup("list-only"))
}

func fetchRedHatAPI(cmd *cobra.Command, args []string) (err error) {
	log15.Info("Initialize Database")
	driver, locked, err := db.NewDB(viper.GetString("dbtype"), viper.GetString("dbpath"), viper.GetBool("debug-sql"))
	if err != nil {
		if locked {
			log15.Error("Failed to initialize DB. Close DB connection before fetching", "err", err)
		}
		return err
	}

	fetchMeta, err := driver.GetFetchMeta()
	if err != nil {
		log15.Error("Failed to get FetchMeta from DB.", "err", err)
		return err
	}
	if fetchMeta.OutDated() {
		log15.Error("Failed to Insert CVEs into DB. SchemaVersion is old", "SchemaVersion", map[string]uint{"latest": models.LatestSchemaVersion, "DB": fetchMeta.SchemaVersion})
		return xerrors.New("Failed to Insert CVEs into DB. SchemaVersion is old")
	}

	log15.Info("Fetch the list of CVEs")
	entries, err := fetcher.ListAllRedhatCves(
		viper.GetString("before"), viper.GetString("after"), viper.GetInt("threads"))
	if err != nil {
		log15.Error("Failed to fetch the list of CVEs.", "err", err)
		return err
	}
	var resourceURLs []string
	for _, entry := range entries {
		resourceURLs = append(resourceURLs, entry.ResourceURL)
	}

	if viper.GetBool("list-only") {
		for _, e := range entries {
			fmt.Printf("%s\t%s\n", e.CveID, e.PublicDate)
		}
		return nil
	}

	log15.Info(fmt.Sprintf("Fetched %d CVEs", len(entries)))
	cves, err := fetcher.RetrieveRedhatCveDetails(resourceURLs)
	if err != nil {
		log15.Error("Failed to fetch the CVE details.", "err", err)
		return err
	}

	log15.Info("Insert RedHat into DB", "db", driver.Name())
	if err := driver.InsertRedhat(cves); err != nil {
		log15.Error("Failed to insert.", "dbpath", viper.GetString("dbpath"), "err", err)
		return err
	}

	if err := driver.UpsertFetchMeta(fetchMeta); err != nil {
		log15.Error("Failed to upsert FetchMeta to DB.", "dbpath", viper.GetString("dbpath"), "err", err)
		return err
	}

	return nil
}
