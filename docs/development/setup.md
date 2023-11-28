# Local Development Setup

## Requirements

* `go` >= 1.19
* `git`, `make` and `kubectl`
* [Kustomize](https://kustomize.io/)
* Access to a Kubernetes cluster ([Minikube](https://minikube.sigs.k8s.io/docs/), [kind](https://kind.sigs.k8s.io/) or a
  real cluster)

## Clone the Repository

To bring up and start locally the `cloud-provider-ironcore` project for development purposes you first need to clone the repository.

```shell
git clone git@github.com:ironcore-dev/cloud-provider-ironcore.git
cd cloud-provider-ironcore
```

## Install cloud-provider-ironcore into the kind Cluster
For local development with `kind` follow below steps

Copy kubeconfig to apply the config in kind

```shell
kind get kubeconfig > ./config/kind/kubeconfig
```
Create cloud-config file under ./config/kind/ with the help of sample file present under ./config/sample/cloud-config 

Build and load the cloud controller into the kind cluster, then apply the config.

```shell
make docker-build
kustomize build config/kind | kubectl apply -f -
```

**Note**: In case that there are multiple environments running, ensure that `kind get clusters` is pointing to the
default kind cluster.

## Cleanup

To remove the cloud-controller from your cluster, simply run

```shell
kustomize build config/kind | kubectl delete -f -
```