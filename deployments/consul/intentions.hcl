# Consul Service Intentions — Traffic Control
#
# These define which services are allowed to communicate with each other.
# With Consul Connect + Envoy, any call not listed here is denied at the
# sidecar level (mTLS cert will be rejected), even if network routing allows it.
#
# Apply with: consul config write deployments/consul/intentions.hcl

Kind = "service-intentions"
Name = "iam-service"
Sources = [
  # api-gateway validates every JWT by calling iam-service
  { Name = "api-gateway",     Action = "allow" },
  # document-worker uses service account tokens
  { Name = "document-worker", Action = "allow" },
  { Name = "*",               Action = "deny"  },
]

---

Kind = "service-intentions"
Name = "driver-service"
Sources = [
  { Name = "api-gateway",      Action = "allow" },
  { Name = "dispatch-service", Action = "allow" },
  { Name = "document-worker",  Action = "allow" },
  { Name = "*",                Action = "deny"  },
]

---

Kind = "service-intentions"
Name = "dispatch-service"
Sources = [
  { Name = "api-gateway",     Action = "allow" },
  { Name = "billing-service", Action = "allow" },
  { Name = "*",               Action = "deny"  },
]

---

Kind = "service-intentions"
Name = "tracking-service"
Sources = [
  { Name = "api-gateway",      Action = "allow" },
  { Name = "dispatch-service", Action = "allow" },
  { Name = "*",                Action = "deny"  },
]

---

Kind = "service-intentions"
Name = "billing-service"
Sources = [
  { Name = "api-gateway",  Action = "allow" },
  { Name = "*",            Action = "deny"  },
]

---

Kind = "service-intentions"
Name = "notification-service"
Sources = [
  { Name = "api-gateway",      Action = "allow" },
  { Name = "billing-service",  Action = "allow" },
  { Name = "driver-service",   Action = "allow" },
  { Name = "dispatch-service", Action = "allow" },
  { Name = "*",                Action = "deny"  },
]
