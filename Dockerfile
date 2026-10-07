ARG NODE_VERSION=24-alpine
ARG PNPM_VERSION=11.20.0

FROM node:${NODE_VERSION} AS base
ARG PNPM_VERSION
RUN corepack enable && corepack prepare pnpm@${PNPM_VERSION} --activate
WORKDIR /app

# ---- dependencies (all, needed for build) ----
FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY prisma ./prisma
COPY prisma7.config.ts ./
RUN pnpm install --frozen-lockfile

# ---- build ----
FROM base AS build
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN pnpm run prisma:generate
RUN pnpm run build
RUN pnpm prune --prod

# ---- runtime ----
FROM node:${NODE_VERSION} AS runner
ENV NODE_ENV=production
WORKDIR /app
RUN addgroup -S nodejs && adduser -S nestjs -G nodejs

COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY --from=build /app/prisma ./prisma
COPY --from=build /app/prisma7.config.ts ./
COPY --from=build /app/package.json ./
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh && chown -R nestjs:nodejs /app

USER nestjs

EXPOSE 3000

ENTRYPOINT ["sh", "/app/docker-entrypoint.sh"]
CMD ["node", "dist/main"]
