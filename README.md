🫧 Blobstr Relay
=================

A specialized Nostr relay + Blossom server built for selective file/media sharing.

It allows users to upload files, and make them only accessible to other users who are tagged in [NIP-94 file metadata events](https://github.com/nostr-protocol/nips/blob/master/94.md).

Built with [Khatru](https://github.com/fiatjaf/khatru), using some of its example code as a base.


**Note:** This is still highly experimental, and mostly a proof of concept for now. There are likely bugs, security issues, and things might break. Please proceed with caution.

### Use Cases

- Share images with a group of friends/family, from a home server (or another trusted server)
- Social recovery of files/images between a group of people
- Share files within a business team, with granular access control

### Features

- Limit who can upload files to the server
- Limit file upload size
- Allow uploaders to specify who can read the file via NIP-94 metadata

### Limitations

- This mostly assumes that the server is owned and trusted by the uploaders (e.g. a home server, or a self-hosted server). It does not support end-to-end encryption, and the server operator can see all files uploaded to the server.
- This leaks metadata about who is sharing files with whom, and when, to whoever has access to the relay.
- This is still highly experimental, may contain bugs, and not yet recommended for production use.

## Getting started

1. Clone this repository
2. Copy `.env.example` to `.env` and fill in the required values
3. Run `go run main.go` to start the server

## Example usage

For the following example, we will use the following tools:
- [nak](https://github.com/fiatjaf/nak)
- [blossom-cli](https://git.fiatjaf.com/blossom), with a custom patch to support authentication on download requests (not yet merged)
- [jq](https://jqlang.org)

We are also assuming that the server is configured on `localhost:3334`

### 1. Generate two nostr private keys

```sh
KEY_1=$(nak key generate)
KEY_2=$(nak key generate)
```

### 2. Save their public keys

```sh
PUBKEY_1=$(nak key public $KEY_1)
PUBKEY_2=$(nak key public $KEY_2)
```

### 3. Add user 1 to the `ALLOWED_USERS` in the `.env` file

```sh
echo "ALLOWED_USERS=$PUBKEY_1" >> .env
```

### 4. Start the server

```sh
go run main.go
```

### 5. Upload a file using `KEY_2` (will fail)

```sh
blossom upload --sec=$KEY_2 --server http://localhost:3334 ~/Downloads/icon.png
```

### 6. Upload a file using `KEY_1` (will succeed)

```sh
blossom upload --sec=$KEY_1 --server http://localhost:3334 ~/Downloads/icon.png
```

This returns:

```json
{"url":"http://localhost:3334/9e0db9b76dddf8cd69c9778993da62e5975634d38583a2ac87536488bd4e82fe.png","sha256":"9e0db9b76dddf8cd69c9778993da62e5975634d38583a2ac87536488bd4e82fe","size":2534522,"type":"image/png","uploaded":1741135365}
```

### 7. Try to download the file using `KEY_2` (will fail with code 403)

```sh
blossom download --sec=$KEY_2 --server http://localhost:3334 9e0db9b76dddf8cd69c9778993da62e5975634d38583a2ac87536488bd4e82fe > ~/Downloads/download.png
```

This returns:

```
9e0db9b76dddf8cd69c9778993da62e5975634d38583a2ac87536488bd4e82fe is not present in http://localhost:3334: 403
```

### 8. Share the file with `KEY_2` using a NIP-94 event signed by `KEY_1`

```sh
nak event --kind 1063 --tag x=9e0db9b76dddf8cd69c9778993da62e5975634d38583a2ac87536488bd4e82fe --tag p=$PUBKEY_2 --sec $KEY_1 ws://localhost:3334
```

_Note:_ The event above is not fully NIP-94 as it is missing some required fields. This is just a simplified example that works with the current implementation.

### 9. List files shared with `KEY_2`

```sh
nak req --kind=1063 --tag p=$PUBKEY_2 http://localhost:3334 | jq '.tags[] | select(.[0] == "x") | .[1]'
```

This will return:

```json
"9e0db9b76dddf8cd69c9778993da62e5975634d38583a2ac87536488bd4e82fe"
```

### 7. Try to download the file using `KEY_2` again

```sh
blossom download --sec=$KEY_2 --server http://localhost:3334 9e0db9b76dddf8cd69c9778993da62e5975634d38583a2ac87536488bd4e82fe > ~/Downloads/download.png
```

This will download the file successfully.

## Contributing

Contributions are welcome! Please open an issue or a pull request if you would like to contribute.

Please use `git commit --signoff` when committing changes to this repository, to certify that you agree to the [Developer Certificate of Origin](DCO.txt).
