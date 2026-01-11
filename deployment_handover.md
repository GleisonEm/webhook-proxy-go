# Kubernetes Deployment Standard (K3s/Hostinger)

This document describes the deployment standard used for the backend, to be replicated for the Frontend or other services.

## Infrastructure Context

- **Provider**: Hostinger VPS
- **Kubernetes Distro**: K3s (Single Node)
- **Ingress Controller**: Traefik (Built-in)
- **Cert Manager**: ClusterIssuer `letsencrypt-prod`
- **Container Registry**: Docker Hub (Public/Private)
- **CI/CD**: GitHub Actions -> `kubectl apply`

## 1. Structure: Helm Chart

The manifests are no longer static. They live in `infra/helm/charts/webhook-proxy/templates/`.

- **`infra/helm/values.yaml`**: Default values (inside chart).
- **`infra/helm/values-prod.yaml`**: Overrides for Production (e.g., 3 replicas, prod domain).
- **`infra/helm/values-dev.yaml`**: Overrides for Development (e.g., 1 replica, dev domain).

- **Liveness/Readiness Probes**: Essential for zero-downtime deployments.
- **EnvVars**: Use `envFrom: secretRef` to load sensitive data from a manually created Secret on the server.
- **Service**:
  - Type: `ClusterIP` (Internal only).
  - Port 80 targets the container port.

## 2. Secrets Management

**DO NOT** commit `.env` files.

1.  **On the Server**: Create the Secret using an `.env` file locally:
    ```bash
    kubectl create secret generic <service-name>-env \
      --namespace <namespace> \
      --from-env-file=.env \
      --dry-run=client -o yaml | kubectl apply -f -
    ```
2.  **Reference in values**: Use the variable `envSecretName: <service-name>-env`.

## 3. CI/CD Workflow (`.github/workflows/deploy.yml`)

The pipeline does NOT use SSH to run docker-compose. It uses `kubectl`.

**Required GitHub Secrets:**

- `DOCKER_USERNAME`: Your Docker Hub user.
- `DOCKER_PASSWORD`: Your Docker Hub token/password.
- `KUBECONFIG`: The contents of `~/.kube/config` (or `/etc/rancher/k3s/k3s.yaml`) from the VPS. **Important:** Change the server IP from `127.0.0.1` to the **VPS Public IP**.

**Workflow Steps:**

1.  **Checkout code**.
2.  **Login to Docker Hub**.
3.  **Build and Push** Docker image.
4.  **Install kubectl** (using `azure/setup-kubectl`).
5.  **Set Kubeconfig** (using `azure/k8s-set-context`).
6.  **Deploy**:
    **Command:**

```bash
helm upgrade --install <release-name> ./infra/helm/charts/webhook-proxy \
  -f ./infra/helm/values-<env>.yaml \
  --set image.tag=latest \
  --wait
```

## 4. How to Deploy a New Environment locally

To test a deploy for "dev" manually:

```bash
helm upgrade --install webhook-proxy-dev ./infra/helm/charts/webhook-proxy -f ./infra/helm/values-dev.yaml
```

- **Inspect Start Errors**: `kubectl describe pod <pod-name>` (Look at "Events" at the bottom).
- **View Logs**: `kubectl logs -l app=<app-label> -f`
