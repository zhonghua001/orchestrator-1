/*
   Copyright 2015 Shlomi Noach, courtesy Booking.com

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package inst

import (
	"fmt"

	"github.com/github/orchestrator/go/db"
	"github.com/openark/golib/log"
	"github.com/openark/golib/sqlutils"
)

// ReadClusterNameByAlias
func ReadClusterNameByAlias(alias string) (clusterName string, err error) {
	query := `
		select
			cluster_name
		from
			cluster_alias
		where
			alias = ?
			or cluster_name = ?
		`
	err = db.QueryOrchestrator(query, sqlutils.Args(alias, alias), func(m sqlutils.RowMap) error {
		clusterName = m.GetString("cluster_name")
		return nil
	})
	if err != nil {
		return "", err
	}
	if clusterName == "" {
		err = fmt.Errorf("No cluster found for alias %s", alias)
	}
	return clusterName, err
}

// ReadAliasByClusterName returns the cluster alias for the given cluster name,
// or the cluster name itself if not explicit alias found
func ReadAliasByClusterName(clusterName string) (alias string, err error) {
	alias = clusterName // default return value
	query := `
		select
			alias
		from
			cluster_alias
		where
			cluster_name = ?
		`
	err = db.QueryOrchestrator(query, sqlutils.Args(clusterName), func(m sqlutils.RowMap) error {
		alias = m.GetString("alias")
		return nil
	})
	return clusterName, err
}

// WriteClusterAlias will write (and override) a single cluster name mapping
func WriteClusterAlias(clusterName string, alias string) error {
	writeFunc := func() error {
		_, err := db.ExecOrchestrator(`
			replace into
					cluster_alias (cluster_name, alias, last_registered)
				values
					(?, ?, now())
			`,
			clusterName, alias)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// WriteClusterAliasManualOverride will write (and override) a single cluster name mapping
func WriteClusterAliasManualOverride(clusterName string, alias string) error {
	writeFunc := func() error {
		_, err := db.ExecOrchestrator(`
			replace into
					cluster_alias_override (cluster_name, alias)
				values
					(?, ?)
			`,
			clusterName, alias)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// UpdateClusterAliases writes down the cluster_alias table based on information
// gained from database_instance
func UpdateClusterAliases() error {
	writeFunc := func() error {
		_, err := db.ExecOrchestrator(`
			replace into
					cluster_alias (alias, cluster_name, last_registered)
				select
				    suggested_cluster_alias,
						cluster_name,
						now()
					from
				    database_instance
				    left join database_instance_downtime using (hostname, port)
				  where
				    suggested_cluster_alias!=''
						/* exclude newly demoted, downtimed masters */
						and ifnull(
								database_instance_downtime.downtime_active = 1
								and database_instance_downtime.end_timestamp > now()
								and database_instance_downtime.reason = ?
							, 0) = 0
					order by
						ifnull(last_checked <= last_seen, 0) asc,
						read_only desc,
						num_slave_hosts asc
			`, DowntimeLostInRecoveryMessage)
		return log.Errore(err)
	}
	if err := ExecDBWriteFunc(writeFunc); err != nil {
		return err
	}
	writeFunc = func() error {
		// Handling the case where no cluster alias exists: we write a dummy alias in the form of the real cluster name.
		_, err := db.ExecOrchestrator(`
			replace into
					cluster_alias (alias, cluster_name, last_registered)
				select
						cluster_name, cluster_name, now()
				  from
				    database_instance
				  group by
				    cluster_name
					having
						sum(suggested_cluster_alias = '') = count(*)
			`)
		return log.Errore(err)
	}
	if err := ExecDBWriteFunc(writeFunc); err != nil {
		return err
	}
	return nil
}

// ReplaceAliasClusterName replaces alis mapping of one cluster name onto a new cluster name.
// Used in topology failover/recovery
func ReplaceAliasClusterName(oldClusterName string, newClusterName string) (err error) {
	{
		writeFunc := func() error {
			_, err := db.ExecOrchestrator(`
			update cluster_alias
				set cluster_name = ?
				where cluster_name = ?
			`,
				newClusterName, oldClusterName)
			return log.Errore(err)
		}
		err = ExecDBWriteFunc(writeFunc)
	}
	{
		writeFunc := func() error {
			_, err := db.ExecOrchestrator(`
			update cluster_alias_override
				set cluster_name = ?
				where cluster_name = ?
			`,
				newClusterName, oldClusterName)
			return log.Errore(err)
		}
		if ferr := ExecDBWriteFunc(writeFunc); ferr != nil {
			err = ferr
		}
	}
	return err
}
