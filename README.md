# vault-policies
A simple tool to synchronize your vault policies from your code repository with your server.

# Install

To install this tool, you should run the following command:
```
$ go install github.com/fynelabs/vault-policies@latest
```

## Initialize
If you are already using vault, it is likely that you have setup some policies. You might want to get them locally as a starting point. To do so, you can do the following with the _backup_ command:
```
$ vault login
[...]
$ vault-policies backup toyour/directory
```

This can also be useful for regularly getting a snapshot of the policies in production for audit or just backup.

## Seting rules on your server
If you do not want any rules to be removed and just update the rules you have defined in your directory to be replicated on your vault instance, you should use the _upload_ command as follow:
```
$ vault login
[...]
$ vault-policies upload fromyour/directory
```

If you want to have the rules set on your server to exactly and strictly match the one defined in your directory, you should use the _restore_ command as follow:
```
$ vault login
[...]
$ vault-policies restore fromyour/directory
```

# License
This code is under MPL-2 as is vault to facilitate adoption.
