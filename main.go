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
		Description: "vault-policies is a tool to keep your vault policies synchronized with your code and easier to integrate in your release process.",
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
				Name:  "backup",
				Usage: "Backup your policies from a Vault into the specified local directory",
				Action: func(c *cli.Context) error {
					if len(c.Args().Slice()) != 1 {
						return fmt.Errorf("backup requires a directory")
					}

					directory := c.Args().Slice()[0]

					return backupPolicies(dev, dryRun, directory)
				},
			},
			{
				Name:  "upload",
				Usage: "Upload policies from a directory into Vault (will overwrite existing policies, but won't remove any existing policies)",
				Action: func(c *cli.Context) error {
					if len(c.Args().Slice()) != 1 {
						return fmt.Errorf("upload requires a directory")
					}

					directory := c.Args().Slice()[0]

					return uploadPolicies(dev, dryRun, directory)
				},
			},
			{
				Name:  "restore",
				Usage: "Restore your policies from a local directory into Vault (will overwrite existing policies, and remove any existing policies not present in the local directory)",
				Action: func(c *cli.Context) error {
					if len(c.Args().Slice()) != 1 {
						return fmt.Errorf("restore requires a directory")
					}

					directory := c.Args().Slice()[0]

					return restorePolicies(dev, dryRun, directory)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func backupPolicies(dev, dryRun bool, directory string) error {
	log("Backing policies to", directory)
	client, err := selectNewVault(dev)
	if err != nil {
		return err
	}

	err = walkRemotePolicies(client, func(policy, content string) error {
		if dryRun {
			fmt.Printf("Would have written %s.hcl with content:\n", policy)
			fmt.Println(content)
		} else {
			log(fmt.Sprintf("Writing %s.hcl", policy))
			err = os.WriteFile(filepath.Join(directory, policy+".hcl"), []byte(content), 0644)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	log("Done backing up")
	return nil
}

func uploadPolicies(dev, dryRun bool, directory string) error {
	log("Uploading policies from", directory)
	client, err := selectNewVault(dev)
	if err != nil {
		return err
	}

	log("Walking directory", directory)
	defer log("Done uploading policies")
	return walkDirectoryPolicies(directory, func(policy string, content []byte) error {
		if dryRun {
			fmt.Printf("Would have written policy %s with content:\n", policy)
			fmt.Println(string(content))
		} else {
			log("Setting policy", policy)
			client.Sys().PutPolicy(policy, string(content))
		}

		return nil
	})
}

func restorePolicies(dev, dryRun bool, directory string) error {
	log("Restoring policies from", directory)
	client, err := selectNewVault(dev)
	if err != nil {
		return err
	}

	remotePolicies := make(map[string]string)

	err = walkRemotePolicies(client, func(policy, content string) error {
		remotePolicies[policy] = content
		return nil
	})
	if err != nil {
		return err
	}

	localPolicies := make(map[string]string)

	log("Walking directory", directory)
	err = walkDirectoryPolicies(directory, func(policy string, content []byte) error {
		log("Found policy", policy)
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
			log("Setting policy", policy)
			client.Sys().PutPolicy(policy, localPolicies[policy])
		}
	}

	log("Done restoring policies")
	return nil
}

func walkDirectoryPolicies(directory string, f func(policy string, content []byte) error) error {
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

func walkRemotePolicies(client *vaultApi.Client, f func(policy string, content string) error) error {
	log("Listing policies from the Vault server")
	policies, err := client.Sys().ListPolicies()
	if err != nil {
		return err
	}

	for _, policy := range policies {
		log("Getting policy", policy)
		content, err := client.Sys().GetPolicy(policy)
		if err != nil {
			return err
		}

		err = f(policy, content)
		if err != nil {
			return err
		}
	}

	return nil
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
