# Feedback

These are some thoughts from the Hackfest with Microsoft November 7-9.

We're currently exposing the buckets a cluster has in the Kubernetes config.  The operator destroys buckets that aren't in the config.  This leads to some non-intuitive behavior that surprised the team.  For instance, when creating a sample bucket, it briefly appears and is then deleted.  Baking an opinion on what buckets a cluster may be too low level.  An alternative would be to be opinionated about the nodes in the cluster, but leave buckets and data up to the user.

Currently the cluster is configured with IPs that are not routable outside the Kubernetes cluster.  This means that XDCR will not function and that any clients using the database must be in the same Kubernetes cluster as the database.  The thought was expressed that a user could customize the deployment to use public DNS in order to support client connectivity and XDCR.  However, that's a substantial amount of work.  An alternative would be to connect to public DNS by default.  This is the approach we take on IaaS in AWS and Azure today.
