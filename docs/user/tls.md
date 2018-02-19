# Couchbase TLS

Couchbase supports transport layer security [TLS] in order to encrypt communications on the wire and provide mutual authentication between peers.

The basic requirements are:

* A certificate authority [CA] certificate which will be used by all actors to validate that peer certificates have been digitally signed by a trusted CA
* A server certificate/key pair for all nodes in the Couchbase cluster.  If using a hierarchy of intermediate CAs these must be appended to the client certificate, ending in the intermediate CA signed by the top-level CA.  Server certificates must have a subject alternative name [SAN] set so that a client can assert that the host name it is connecting to is the same as that in the certificate.  We currently support using wildcard entries only e.g. `*.cluster.domain.svc`, where *cluster* is the name of the *couchbasecluster* resource, and *domain* is the namespace the cluster is running in (typically *default*).
* A client certificate/key pair for all Couchbase operator pods.  Again the certificate is expected to contain all intermediate CAs.

## Configuration

An example configuration is as follows:

```yaml
spec:
  ...
  tls:
    static:
      member:
        serverSecret: couchbase-server-tls
      operatorSecret: couchbase-operator-tls
  ...
```

For static TLS configuration *serverSecret* and *operatorSecret* need to be specified.  These refer to named Kubernetes secrets which are described below.

## Creating Secrets

Secrets are referred to in the *couchbasecluster* resource, therefore may have any name you choose.  The format of individual secrets is discussed below.

### spec.tls.static.member.serverSecret

Server secrets need to be mounted as a volume within the Couchbase Server pod with specific names.  The certificate chain must be named *chain.pem* and the private key *pkey.pem*.

```shell
$ kubectl create secret generic couchbase-server-tls \
  --from-file example/tls/certs/chain.pem \
  --from-file example/tls/certs/pkey.key
```

### spec.tls.static.operatorSecret

The operator client secrets are read directly from the API.  We expect three values to be present; *ca.crt* is the top-level CA which is used to authenticate all TLS certificate chains, *couchbase_operator.crt* is the certificate chain for the operator, and *couchbase-operator.key* is the private key for the operator.

```shell
$ kubectl create secret generic couchbase-operator-tls \
  --from-file example/tls/certs/ca.crt \
  --from-file example/tls/certs/couchbase-operator.crt \
  --from-file example/tls/certs/couchbase-operator.key
```

## Creating Certificates

Creating X.509 certificates is beyond the scope of this documentation and is given only for illustrative purposes only.

### EasyRSA

EasyRSA by OpenVPN makes operating a public key infrastructure [PKI] relatively simple, and is the recommended method to get up and running quickly.

First clone the repository:

```shell
$ git clone https://github.com/OpenVPN/easy-rsa
```

Initialize and create the CA certificate/key.  You will be prompted for a private key password and the CA common name [CN], something like *Couchbase CA* is sufficient.  The CA certificate will be available as *pki/ca.crt*.

```shell
$ cd easy-rsa/easyrsa3
$ ./easyrsa init-pki
$ ./easyrsa build-ca
```

Create a server wildcard certificate and key to be used on Couchbase Server pods.  The operator/clients will access the pods via Kubernetes services for example *cb-example-0000.cb-example.default.svc* so this needs to be in the SAN list in order for a client to verify the certificate belongs to the host being connected to.  To this end we add in a *--subject-alt-name*, this can be specified multiple times in case your client uses a different method of addressing.  The key/certificate pair can be found in *pki/private/couchbase-server.key* and *pki/issued/coouchbase-server.crt* and used as *pkey.pem* and in the *chain.pem* files respectively in the *serverSecret*.

```shell
$ ./easyrsa --subject-alt-name=DNS:*.cb-example.default.svc build-server-full couchbase-server nopass
```

Finally for all clients, including the operator itself, create a client key/certificate pair.  These files can be found in the directories listed above and can be used directly in the *operatorSecret*.

```shell
$ ./easyrsa build-client-full couchbase-operator nopass
```

Please note that password protected keys are supported by neither Couchbase Server nor Couchbase Operator.

#### Private Key Formatting

Due to an [issue](https://issues.couchbase.com/browse/MB-24404) with Couchbase server's private key handling, server keys need to be PKCS#1 formatted.  This can be achieved with the following commands:

```shell
$ openssl rsa -in pkey.key -out pkey.key.der -outform DER
$ openssl rsa -in pkey.key.der -inform DER -out pkey.key -outform PEM
```
