# Deploy manually

## Requirements

* `go` >= 1.19
* `git`, `make` and `kubectl`
* [Kustomize](https://kustomize.io/)
* Access to a Kubernetes cluster ([Minikube](https://minikube.sigs.k8s.io/docs/), [kind](https://kind.sigs.k8s.io/) or a
  real [kubeadm](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/install-kubeadm/) cluster)
* Update the kubelet service on each node with Args ``--cloud-provider=external``
  Follow below steps to update the kubelet service environment variable ``KUBELET_KUBECONFIG_ARGS``
    * Open ``/etc/systemd/system/kubelet/10-kubeadm.conf``
    * Add ``--cloud-provider=external``  into above config file 
    
    example: 
    ```
    Environment="KUBELET_KUBECONFIG_ARGS=--bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --cloud-provider=external --kubeconfig=/etc/kubernetes/kubelet.conf"
    ```
    
     * Restart the kubelet service
     ```
     $ systemctl daemon-reload
     $ systemctl restart kubelet
     ```
    
## Steps to Deploy "Cloud-provider-ironcore"
* To deploy clone the repository [cloud-provider-ironcore](https://github.com/ironcore-dev/cloud-provider-ironcore)

```shell
git clone git@github.com:ironcore-dev/cloud-provider-ironcore.git
cd cloud-provider-ironcore
```
* Create folder ``config/kind/ironcore`` and create kubeconfig into folder ``config/kind/ironcore/kubeconfig``.

  If you want to use ironcore server from different cluster then copy kubeconfig of that cluster into folder ``config/kind/ironcore/kubeconfig``

  If you want to use ironcore server from local deployment then copy kubeconfig into folder ``config/kind/ironcore/kubeconfig`` using below command
```shell
kind get kubeconfig > ./config/kind/ironcore/kubeconfig
```
* Copy kubeconfig into folder ``config/kind/kubeconfig``
```shell
kind get kubeconfig > ./config/kind/kubeconfig
```
* Create cloud-config file under ``./config/kind/`` with the help of sample file present under ``./config/sample/cloud-config``

    **Note**: The kubeconfig content here is your ironcore-api cluster's kubeconfig incase of a real kubeadm cluster deployment

* Run below make target to deploy the ``cloud-provider-ironcore``
```shell
make docker-build
kustomize build config/kind | kubectl apply -f -
```
**Validation:**
```
kubectl  get po -n kube-system -o wide| grep ironcore
ironcore-cloud-controller-manager-crws9    1/1     Running   4 (80s ago)     4m13s   10.244.225.76   csi-master
```

**Note**: In case that there are multiple environments running, ensure that `kind get clusters` is pointing to the
default kind cluster.

## Cleanup

To remove the cloud-controller from your cluster, simply run

```shell
kustomize build config/kind | kubectl delete -f -
```