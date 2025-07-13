import { type RouteConfig, index, route } from "@react-router/dev/routes";

export default [
  index("routes/home.tsx"),
  route("login", "routes/login.tsx"),
  route("applications", "routes/applications.tsx"),
  route("status", "routes/status.tsx"),
  route("manage", "routes/manage.tsx"),
] satisfies RouteConfig;
