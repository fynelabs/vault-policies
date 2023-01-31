package main

import (
	"fmt"
	"os"
	"path/filepath"

	vaultApi "github.com/hashicorp/vault/api"
	"github.com/urfave/cli/v2"
)

var debug = false

func main() {
	dev := false
	dryRun := false

	app := &cli.App{
		Name:        "vault-policies",
		Usage:       "An helper to keep vault policies in sync with your code.",
		Description: "vault-policies is a tool to keep your vault policies in sync with your code and easier to integrate in your release process.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "dev",
				Usage:       "Use the dev server",
				Destination: &dev,
			},
			&cli.BoolFlag{
				Name:        "dry-run",
				Usage:       "Don't actually do anything",
				Destination: &dryRun,
			},
			&cli.BoolFlag{
				Name:        "debug",
				Usage:       "Enable debug mode",
				Destination: &debug,
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "import",
				Usage: "Import policies from a Vault into the specified directory",
				Action: func(c *cli.Context) error {
					if len(c.Args().Slice()) != 1 {
						return fmt.Errorf("import requires a directory")
					}

					directory := c.Args().Slice()[0]

					return importPolicies(dev, dryRun, directory)
				},
			},
			{
				Name:  "export",
				Usage: "Export policies from a directory into Vault (will overwrite existing policies, but won't remove any existing policies)",
				Action: func(c *cli.Context) error {
					if len(c.Args().Slice()) != 1 {
						return fmt.Errorf("export requires a directory")
					}

					directory := c.Args().Slice()[0]

					return exportPolicies(dev, dryRun, directory)
				},
			},
			{
				Name:  "sync",
				Usage: "Synchronize policies from a directory into Vault (will overwrite existing policies, and remove any existing policies not present in the directory)",
				Action: func(c *cli.Context) error {
					if len(c.Args().Slice()) != 1 {
						return fmt.Errorf("sync requires a directory")
					}

					directory := c.Args().Slice()[0]

					return synchronizePolicies(dev, dryRun, directory)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func importPolicies(dev, dryRun bool, directory string) error {
	log("Importing policies from %s", directory)
	client, err := selectNewVault(dev)
	if err != nil {
		return err
	}

	log("Listing policies from the Vault server")
	policies, err := client.Sys().ListPolicies()
	if err != nil {
		return err
	}

	for _, policy := range policies {
		log("Getting policy %s", policy)
		details, err := client.Sys().GetPolicy(policy)
		if err != nil {
			return err
		}

		if dryRun {
			fmt.Printf("Would have written %s.hcl with content:\n", policy)
			fmt.Println(details)
		} else {
			log("Writing %s.hcl", policy)
			err = os.WriteFile(filepath.Join(directory, policy+".hcl"), []byte(details), 0644)
			if err != nil {
				return err
			}
		}
	}

	log("Done importing policies to %s", directory)
	return nil
}

func exportPolicies(dev, dryRun bool, directory string) error {
	log("Exporting policies from %s", directory)
	client, err := selectNewVault(dev)
	if err != nil {
		return err
	}

	log("Walking directory %s", directory)
	defer log("Done exporting policies from %s", directory)
	return walkPoliciesDirectory(directory, func(policy string, content []byte) error {
		if dryRun {
			fmt.Printf("Would have written policy %s with content:\n", policy)
			fmt.Println(string(content))
		} else {
			log("Setting policy %s", policy)
			client.Sys().PutPolicy(policy, string(content))
		}

		return nil
	})
}

func synchronizePolicies(dev, dryRun bool, directory string) error {
	log("Synchronizing policies from %s", directory)
	client, err := selectNewVault(dev)
	if err != nil {
		return err
	}

	log("Listing policies from the Vault server")
	policies, err := client.Sys().ListPolicies()
	if err != nil {
		return err
	}

	remotePolicies := make(map[string]string)

	for _, policy := range policies {
		log("Getting policy %s", policy)
		details, err := client.Sys().GetPolicy(policy)
		if err != nil {
			return err
		}

		remotePolicies[policy] = details
	}

	localPolicies := make(map[string]string)

	log("Walking directory %s", directory)
	err = walkPoliciesDirectory(directory, func(policy string, content []byte) error {
		log("Found policy %s", policy)
		localPolicies[policy] = string(content)
		return nil
	})
	if err != nil {
		return err
	}

	log("Deleting policies not present in the directory")
	for policy := range remotePolicies {
		if _, ok := localPolicies[policy]; !ok {
			if dryRun {
				fmt.Printf("Would have deleted policy %s\n", policy)
			} else {
				client.Sys().DeletePolicy(policy)
			}
		}
	}

	log("Writing policies back to the Vault server when needed")
	for policy := range localPolicies {
		if _, ok := remotePolicies[policy]; ok {
			if remotePolicies[policy] == localPolicies[policy] {
				continue
			}
		}

		if dryRun {
			fmt.Printf("Would have written policy %s with content:\n", policy)
			fmt.Println(localPolicies[policy])
		} else {
			log("Setting policy %s", policy)
			client.Sys().PutPolicy(policy, localPolicies[policy])
		}
	}

	log("Done synchronizing policies")
	return nil
}

func walkPoliciesDirectory(directory string, f func(policy string, content []byte) error) error {
	return filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".hcl" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Guess the policy name from the file name
		policy := filepath.Base(path)
		policy = policy[:len(policy)-len(filepath.Ext(policy))]

		return f(policy, content)
	})
}

func newVault(address string, token string, CAPath string, ClientCert string, ClientKey string) (*vaultApi.Client, error) {
	config := vaultApi.DefaultConfig()

	config.Address = address

	if CAPath != "" && ClientCert != "" && ClientKey != "" {
		config.ConfigureTLS(&vaultApi.TLSConfig{
			CACert:     CAPath,
			ClientCert: ClientCert,
			ClientKey:  ClientKey,
		})
	}

	client, err := vaultApi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize Vault developer client: %w", err)
	}

	client.SetToken(token)

	return client, nil
}

func newVaultDev() (*vaultApi.Client, error) {
	return newVault("http://127.0.0.1:8200", "dev-only-token", "", "", "")
}

func selectNewVault(dev bool) (*vaultApi.Client, error) {
	if dev {
		return newVaultDev()
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	token, err := os.ReadFile(filepath.Join(home, ".vault-token"))
	if err != nil {
		return nil, err
	}

	return newVault(os.Getenv("VAULT_ADDR"), string(token),
		os.Getenv("VAULT_CACERT"),
		os.Getenv("VAULT_CLIENT_CERT"),
		os.Getenv("VAULT_CLIENT_KEY"))
}

func log(message ...string) {
	if debug {
		fmt.Println(message)
	}
}
