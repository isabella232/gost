package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/go-redis/redis/v8"
	"github.com/inconshreveable/log15"
	"github.com/knqyf263/gost/config"
	"github.com/knqyf263/gost/models"
	"github.com/labstack/gommon/log"
	"github.com/spf13/viper"
	"golang.org/x/xerrors"
)

/**
# Redis Data Structure

- HASH
  ┌───┬────────────┬──────────────────────────────────┬──────────┬─────────────────────────────────┐
  │NO │    HASH    │         FIELD                    │  VALUE   │             PURPOSE             │
  └───┴────────────┴──────────────────────────────────┴──────────┴─────────────────────────────────┘
  ┌───┬────────────┬──────────────────────────────────┬──────────┬─────────────────────────────────┐
  │ 1 │CVE#$CVEID  │  RedHat/Debian/Ubuntu/Microsoft  │ $CVEJSON │     TO GET CVEJSON BY CVEID     │
  └───┴────────────┴──────────────────────────────────┴──────────┴─────────────────────────────────┘


- ZINDE  X
  ┌───┬────────────────┬──────────┬────────────┬───────────────────────────────────────────┐
  │NO │    KEY         │  SCORE   │  MEMBER    │                PURPOSE                    │
  └───┴────────────────┴──────────┴────────────┴───────────────────────────────────────────┘
  ┌───┬────────────────┬──────────┬────────────┬───────────────────────────────────────────┐
  │ 1 │CVE#R#$PKGNAME  │    0     │  $CVEID    │(RedHat) GET RELATED []CVEID BY PKGNAME    │
  ├───┼────────────────┼──────────┼────────────┼───────────────────────────────────────────┤
  │ 2 │CVE#D#$PKGNAME  │    0     │  $CVEID    │(Debian) GET RELATED []CVEID BY PKGNAME    │
  ├───┼────────────────┼──────────┼────────────┼───────────────────────────────────────────┤
  │ 3 │CVE#U#$PKGNAME  │    0     │  $CVEID    │(Ubuntu) GET RELATED []CVEID BY PKGNAME    │
  ├───┼────────────────┼──────────┼────────────┼───────────────────────────────────────────┤
  │ 3 │CVE#K#$KBID     │    0     │  $CVEID    │(Microsoft) GET RELATED []CVEID BY KBID    │
  ├───┼────────────────┼──────────┼────────────┼───────────────────────────────────────────┤
  │ 4 │CVE#P#$PRODUCTID│    0     │$PRODUCTNAME│(Microsoft) GET RELATED []PRODUCTNAME BY ID│
  └───┴────────────────┴──────────┴────────────┴───────────────────────────────────────────┘

**/

const (
	dialectRedis                 = "redis"
	hashKeyPrefix                = "CVE#"
	zindRedHatPrefix             = "CVE#R#"
	zindDebianPrefix             = "CVE#D#"
	zindUbuntuPrefix             = "CVE#U#"
	zindMicrosoftKBIDPrefix      = "CVE#K#"
	zindMicrosoftProductIDPrefix = "CVE#P#"
)

// RedisDriver is Driver for Redis
type RedisDriver struct {
	name string
	conn *redis.Client
}

// Name return db name
func (r *RedisDriver) Name() string {
	return r.name
}

// OpenDB opens Database
func (r *RedisDriver) OpenDB(dbType, dbPath string, debugSQL bool) (locked bool, err error) {
	if err = r.connectRedis(dbPath); err != nil {
		err = fmt.Errorf("Failed to open DB. dbtype: %s, dbpath: %s, err: %s", dbType, dbPath, err)
	}
	return
}

func (r *RedisDriver) connectRedis(dbPath string) error {
	ctx := context.Background()
	var err error
	var option *redis.Options
	if option, err = redis.ParseURL(dbPath); err != nil {
		log15.Error("Failed to parse url.", "err", err)
		return err
	}
	r.conn = redis.NewClient(option)
	err = r.conn.Ping(ctx).Err()
	return err
}

// CloseDB close Database
func (r *RedisDriver) CloseDB() (err error) {
	if r.conn == nil {
		return
	}
	if err = r.conn.Close(); err != nil {
		return xerrors.Errorf("Failed to close DB. Type: %s. err: %w", r.name, err)
	}
	return
}

// MigrateDB migrates Database
func (r *RedisDriver) MigrateDB() error {
	return nil
}

// IsGostModelV1 determines if the DB was created at the time of Gost Model v1
func (r *RedisDriver) IsGostModelV1() (bool, error) {
	return false, nil
}

// GetFetchMeta get FetchMeta from Database
func (r *RedisDriver) GetFetchMeta() (*models.FetchMeta, error) {
	return &models.FetchMeta{GostRevision: config.Revision, SchemaVersion: models.LatestSchemaVersion}, nil
}

// UpsertFetchMeta upsert FetchMeta to Database
func (r *RedisDriver) UpsertFetchMeta(*models.FetchMeta) error {
	return nil
}

// GetAfterTimeRedhat :
func (r *RedisDriver) GetAfterTimeRedhat(time.Time) ([]models.RedhatCVE, error) {
	return nil, fmt.Errorf("Not implemented yet")
}

// GetRedhat :
func (r *RedisDriver) GetRedhat(cveID string) *models.RedhatCVE {
	ctx := context.Background()
	result := r.conn.HGetAll(ctx, hashKeyPrefix+cveID)
	if result.Err() != nil {
		log15.Error("Failed to get cve.", "err", result.Err())
		return nil
	}

	var redhat models.RedhatCVE
	if j, ok := result.Val()["RedHat"]; ok {
		if err := json.Unmarshal([]byte(j), &redhat); err != nil {
			log15.Error("Failed to Unmarshal json.", "err", err)
			return nil
		}
	}
	return &redhat
}

// GetRedhatMulti :
func (r *RedisDriver) GetRedhatMulti(cveIDs []string) map[string]models.RedhatCVE {
	ctx := context.Background()
	results := map[string]models.RedhatCVE{}
	rs := map[string]*redis.StringStringMapCmd{}

	pipe := r.conn.Pipeline()
	for _, cveID := range cveIDs {
		rs[cveID] = pipe.HGetAll(ctx, hashKeyPrefix+cveID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		if err != redis.Nil {
			log15.Error("Failed to get multi cve json.", "err", err)
			return nil
		}
	}

	for cveID, result := range rs {
		var redhat models.RedhatCVE
		if j, ok := result.Val()["RedHat"]; ok {
			if err := json.Unmarshal([]byte(j), &redhat); err != nil {
				log15.Error("Failed to Unmarshal json.", "err", err)
				return nil
			}
		}
		results[cveID] = redhat
	}
	return results
}

// GetUnfixedCvesRedhat :
func (r *RedisDriver) GetUnfixedCvesRedhat(major, pkgName string, ignoreWillNotFix bool) (m map[string]models.RedhatCVE) {
	ctx := context.Background()
	m = map[string]models.RedhatCVE{}

	var result *redis.StringSliceCmd
	if result = r.conn.ZRange(ctx, zindRedHatPrefix+pkgName, 0, -1); result.Err() != nil {
		log.Error(result.Err())
		return
	}

	cpe := fmt.Sprintf("cpe:/o:redhat:enterprise_linux:%s", major)
	for _, cveID := range result.Val() {
		red := r.GetRedhat(cveID)
		if red == nil {
			log15.Error("CVE is not found", "CVE-ID", cveID)
			continue
		}

		// https://access.redhat.com/documentation/en-us/red_hat_security_data_api/0.1/html-single/red_hat_security_data_api/index#cve_format
		pkgStats := []models.RedhatPackageState{}
		for _, pkgstat := range red.PackageState {
			if pkgstat.Cpe != cpe ||
				pkgstat.PackageName != pkgName ||
				pkgstat.FixState == "Not affected" ||
				pkgstat.FixState == "New" {
				continue

			} else if ignoreWillNotFix && pkgstat.FixState == "Will not fix" {
				continue
			}
			pkgStats = append(pkgStats, pkgstat)
		}
		if len(pkgStats) == 0 {
			continue
		}
		red.PackageState = pkgStats
		m[cveID] = *red
	}
	return
}

// GetUnfixedCvesDebian : get the CVEs related to debian_release.status = 'open', major, pkgName
func (r *RedisDriver) GetUnfixedCvesDebian(major, pkgName string) map[string]models.DebianCVE {
	return r.getCvesDebianWithFixStatus(major, pkgName, "open")
}

// GetFixedCvesDebian : get the CVEs related to debian_release.status = 'resolved', major, pkgName
func (r *RedisDriver) GetFixedCvesDebian(major, pkgName string) map[string]models.DebianCVE {
	return r.getCvesDebianWithFixStatus(major, pkgName, "resolved")
}

func (r *RedisDriver) getCvesDebianWithFixStatus(major, pkgName, fixStatus string) (m map[string]models.DebianCVE) {
	ctx := context.Background()
	m = map[string]models.DebianCVE{}
	codeName, ok := debVerCodename[major]
	if !ok {
		log15.Error("Not supported yet", "major", major)
		return
	}
	var result *redis.StringSliceCmd
	if result = r.conn.ZRange(ctx, zindDebianPrefix+pkgName, 0, -1); result.Err() != nil {
		log.Error(result.Err())
		return
	}

	for _, cveID := range result.Val() {
		deb := r.GetDebian(cveID)
		if deb == nil {
			log15.Error("CVE is not found", "CVE-ID", cveID)
			continue
		}

		pkgs := []models.DebianPackage{}
		for _, pkg := range deb.Package {
			if pkg.PackageName != pkgName {
				continue
			}
			rels := []models.DebianRelease{}
			for _, rel := range pkg.Release {
				if rel.ProductName == codeName && rel.Status == fixStatus {
					rels = append(rels, rel)
				}
			}
			if len(rels) == 0 {
				continue
			}
			pkg.Release = rels
			pkgs = append(pkgs, pkg)
		}
		if len(pkgs) != 0 {
			deb.Package = pkgs
			m[cveID] = *deb
		}
	}
	return
}

// GetDebian :
func (r *RedisDriver) GetDebian(cveID string) *models.DebianCVE {
	ctx := context.Background()
	var result *redis.StringStringMapCmd
	if result = r.conn.HGetAll(ctx, hashKeyPrefix+cveID); result.Err() != nil {
		log.Error(result.Err())
		return nil
	}
	deb := models.DebianCVE{}
	j, ok := result.Val()["Debian"]
	if !ok {
		return nil
	}

	if err := json.Unmarshal([]byte(j), &deb); err != nil {
		log.Errorf("Failed to Unmarshal json. err : %s", err)
		return nil
	}
	return &deb
}

// GetUnfixedCvesUbuntu :
func (r *RedisDriver) GetUnfixedCvesUbuntu(major, pkgName string) map[string]models.UbuntuCVE {
	return r.getCvesUbuntuWithFixStatus(major, pkgName, []string{"needed", "pending"})
}

// GetFixedCvesUbuntu :
func (r *RedisDriver) GetFixedCvesUbuntu(major, pkgName string) map[string]models.UbuntuCVE {
	return r.getCvesUbuntuWithFixStatus(major, pkgName, []string{"released"})
}

func (r *RedisDriver) getCvesUbuntuWithFixStatus(major, pkgName string, fixStatus []string) (m map[string]models.UbuntuCVE) {
	ctx := context.Background()
	m = map[string]models.UbuntuCVE{}
	codeName, ok := ubuntuVerCodename[major]
	if !ok {
		log15.Error("Not supported yet", "major", major)
		return
	}
	var result *redis.StringSliceCmd
	if result = r.conn.ZRange(ctx, zindUbuntuPrefix+pkgName, 0, -1); result.Err() != nil {
		log.Error(result.Err())
		return
	}

	for _, cveID := range result.Val() {
		cve := r.GetUbuntu(cveID)
		if cve == nil {
			log15.Error("CVE is not found", "CVE-ID", cveID)
			continue
		}

		patches := []models.UbuntuPatch{}
		for _, p := range cve.Patches {
			if p.PackageName != pkgName {
				continue
			}
			relPatches := []models.UbuntuReleasePatch{}
			for _, relPatch := range p.ReleasePatches {
				if relPatch.ReleaseName == codeName {
					for _, s := range fixStatus {
						if s == relPatch.Status {
							relPatches = append(relPatches, relPatch)
						}
					}
				}
			}
			if len(relPatches) == 0 {
				continue
			}
			p.ReleasePatches = relPatches
			patches = append(patches, p)
		}
		if len(patches) != 0 {
			cve.Patches = patches
			m[cveID] = *cve
		}
	}
	return
}

// GetUbuntu :
func (r *RedisDriver) GetUbuntu(cveID string) *models.UbuntuCVE {
	ctx := context.Background()
	var result *redis.StringStringMapCmd
	if result = r.conn.HGetAll(ctx, hashKeyPrefix+cveID); result.Err() != nil {
		log.Error(result.Err())
		return nil
	}

	c := models.UbuntuCVE{}
	j, ok := result.Val()["Ubuntu"]
	if !ok {
		return nil
	}

	if err := json.Unmarshal([]byte(j), &c); err != nil {
		xerrors.Errorf("Failed to Unmarshal json. err: %w", err)
		return nil
	}

	return &c
}

// GetMicrosoft :
func (r *RedisDriver) GetMicrosoft(cveID string) *models.MicrosoftCVE {
	ctx := context.Background()
	result := r.conn.HGetAll(ctx, hashKeyPrefix+cveID)
	if result.Err() != nil {
		log15.Error("Failed to get cve.", "err", result.Err())
		return nil
	}

	var ms models.MicrosoftCVE
	if j, ok := result.Val()["Microsoft"]; ok {
		if err := json.Unmarshal([]byte(j), &ms); err != nil {
			log15.Error("Failed to Unmarshal json.", "err", err)
			return nil
		}
	}
	return &ms
}

// GetMicrosoftMulti :
func (r *RedisDriver) GetMicrosoftMulti(cveIDs []string) map[string]models.MicrosoftCVE {
	ctx := context.Background()
	results := map[string]models.MicrosoftCVE{}
	rs := map[string]*redis.StringStringMapCmd{}

	pipe := r.conn.Pipeline()
	for _, cveID := range cveIDs {
		rs[cveID] = pipe.HGetAll(ctx, hashKeyPrefix+cveID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		if err != redis.Nil {
			log15.Error("Failed to get multi cve json.", "err", err)
			return nil
		}
	}

	for cveID, result := range rs {
		var ms models.MicrosoftCVE
		if j, ok := result.Val()["Microsoft"]; ok {
			if err := json.Unmarshal([]byte(j), &ms); err != nil {
				log15.Error("Failed to Unmarshal json.", "err", err)
				return nil
			}
		}
		results[cveID] = ms
	}
	return results
}

//InsertRedhat :
func (r *RedisDriver) InsertRedhat(cveJSONs []models.RedhatCVEJSON) (err error) {
	expire := viper.GetUint("expire")

	ctx := context.Background()
	cves, err := ConvertRedhat(cveJSONs)
	if err != nil {
		return err
	}
	bar := pb.StartNew(len(cves))

	for _, cve := range cves {
		pipe := r.conn.Pipeline()
		bar.Increment()

		j, err := json.Marshal(cve)
		if err != nil {
			return fmt.Errorf("Failed to marshal json. err: %s", err)
		}

		key := hashKeyPrefix + cve.Name
		if result := pipe.HSet(ctx, key, "RedHat", string(j)); result.Err() != nil {
			return fmt.Errorf("Failed to HSet CVE. err: %s", result.Err())
		}
		if expire > 0 {
			if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
				return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
			}
		} else {
			if err := pipe.Persist(ctx, key).Err(); err != nil {
				return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
			}
		}

		for _, pkg := range cve.PackageState {
			key := zindRedHatPrefix + pkg.PackageName
			if result := pipe.ZAdd(
				ctx,
				key,
				&redis.Z{Score: 0, Member: cve.Name},
			); result.Err() != nil {
				return fmt.Errorf("Failed to ZAdd pkg name. err: %s", result.Err())
			}
			if expire > 0 {
				if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
					return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
				}
			} else {
				if err := pipe.Persist(ctx, key).Err(); err != nil {
					return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
				}
			}
		}

		if _, err = pipe.Exec(ctx); err != nil {
			return fmt.Errorf("Failed to exec pipeline. err: %s", err)
		}
	}
	bar.Finish()

	return nil
}

// InsertDebian :
func (r *RedisDriver) InsertDebian(cveJSONs models.DebianJSON) error {
	expire := viper.GetUint("expire")

	ctx := context.Background()
	cves := ConvertDebian(cveJSONs)
	bar := pb.StartNew(len(cves))

	for _, cve := range cves {
		pipe := r.conn.Pipeline()
		bar.Increment()

		j, err := json.Marshal(cve)
		if err != nil {
			return fmt.Errorf("Failed to marshal json. err: %s", err)
		}

		key := hashKeyPrefix + cve.CveID
		if result := pipe.HSet(ctx, key, "Debian", string(j)); result.Err() != nil {
			return fmt.Errorf("Failed to HSet CVE. err: %s", result.Err())
		}
		if expire > 0 {
			if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
				return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
			}
		} else {
			if err := pipe.Persist(ctx, key).Err(); err != nil {
				return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
			}
		}

		for _, pkg := range cve.Package {
			key := zindDebianPrefix + pkg.PackageName
			if result := pipe.ZAdd(
				ctx,
				key,
				&redis.Z{Score: 0, Member: cve.CveID},
			); result.Err() != nil {
				return fmt.Errorf("Failed to ZAdd pkg name. err: %s", result.Err())
			}
			if expire > 0 {
				if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
					return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
				}
			} else {
				if err := pipe.Persist(ctx, key).Err(); err != nil {
					return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
				}
			}
		}

		if _, err = pipe.Exec(ctx); err != nil {
			return fmt.Errorf("Failed to exec pipeline. err: %s", err)
		}
	}
	bar.Finish()
	return nil
}

// InsertUbuntu :
func (r *RedisDriver) InsertUbuntu(cveJSONs []models.UbuntuCVEJSON) (err error) {
	expire := viper.GetUint("expire")

	ctx := context.Background()
	cves := ConvertUbuntu(cveJSONs)
	bar := pb.StartNew(len(cves))

	for _, cve := range cves {
		pipe := r.conn.Pipeline()
		bar.Increment()

		j, err := json.Marshal(cve)
		if err != nil {
			return fmt.Errorf("Failed to marshal json. err: %s", err)
		}

		key := hashKeyPrefix + cve.Candidate
		if result := pipe.HSet(ctx, key, "Ubuntu", string(j)); result.Err() != nil {
			return fmt.Errorf("Failed to HSet CVE. err: %s", result.Err())
		}
		if expire > 0 {
			if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
				return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
			}
		} else {
			if err := pipe.Persist(ctx, key).Err(); err != nil {
				return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
			}
		}

		for _, pkg := range cve.Patches {
			key := zindUbuntuPrefix + pkg.PackageName
			if result := pipe.ZAdd(
				ctx,
				key,
				&redis.Z{Score: 0, Member: cve.Candidate},
			); result.Err() != nil {
				return fmt.Errorf("Failed to ZAdd pkg name. err: %s", result.Err())
			}
			if expire > 0 {
				if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
					return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
				}
			} else {
				if err := pipe.Persist(ctx, key).Err(); err != nil {
					return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
				}
			}
		}

		if _, err = pipe.Exec(ctx); err != nil {
			return fmt.Errorf("Failed to exec pipeline. err: %s", err)
		}
	}
	bar.Finish()
	return nil
}

// InsertMicrosoft :
func (r *RedisDriver) InsertMicrosoft(cveXMLs []models.MicrosoftXML, xls []models.MicrosoftBulletinSearch) (err error) {
	expire := viper.GetUint("expire")

	ctx := context.Background()
	cves, products := ConvertMicrosoft(cveXMLs, xls)
	bar := pb.StartNew(len(cves))

	pipe := r.conn.Pipeline()
	for _, p := range products {
		key := zindMicrosoftProductIDPrefix + p.ProductID
		if result := pipe.ZAdd(
			ctx,
			key,
			&redis.Z{Score: 0, Member: p.ProductName},
		); result.Err() != nil {
			return fmt.Errorf("Failed to ZAdd kbID. err: %s", result.Err())
		}
		if expire > 0 {
			if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
				return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
			}
		} else {
			if err := pipe.Persist(ctx, key).Err(); err != nil {
				return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
			}
		}
	}
	if _, err = pipe.Exec(ctx); err != nil {
		return fmt.Errorf("Failed to exec pipeline. err: %s", err)
	}

	for _, cve := range cves {
		pipe := r.conn.Pipeline()
		bar.Increment()

		j, err := json.Marshal(cve)
		if err != nil {
			return fmt.Errorf("Failed to marshal json. err: %s", err)
		}

		key := hashKeyPrefix + cve.CveID
		if result := pipe.HSet(ctx, key, "Microsoft", string(j)); result.Err() != nil {
			return fmt.Errorf("Failed to HSet CVE. err: %s", result.Err())
		}
		if expire > 0 {
			if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
				return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
			}
		} else {
			if err := pipe.Persist(ctx, key).Err(); err != nil {
				return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
			}
		}

		for _, msKBID := range cve.KBIDs {
			key := zindMicrosoftKBIDPrefix + msKBID.KBID
			if result := pipe.ZAdd(
				ctx,
				key,
				&redis.Z{Score: 0, Member: cve.CveID},
			); result.Err() != nil {
				return fmt.Errorf("Failed to ZAdd kbID. err: %s", result.Err())
			}
			if expire > 0 {
				if err := pipe.Expire(ctx, key, time.Duration(expire*uint(time.Second))).Err(); err != nil {
					return fmt.Errorf("Failed to set Expire to Key. err: %s", err)
				}
			} else {
				if err := pipe.Persist(ctx, key).Err(); err != nil {
					return fmt.Errorf("Failed to remove the existing timeout on Key. err: %s", err)
				}
			}
		}

		if _, err = pipe.Exec(ctx); err != nil {
			return fmt.Errorf("Failed to exec pipeline. err: %s", err)
		}
	}
	bar.Finish()
	return nil
}
