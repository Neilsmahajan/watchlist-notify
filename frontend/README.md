## Watchlist Notify Frontend

Next.js (App Router) frontend for the Watchlist Notify service with Auth0 authentication and modern responsive design.

Status: Active development. Core features implemented with professional UI. Ready for backend integration expansion.

### Stack

- Next.js 15 (App Router, React 19)
- TypeScript with strict configuration
- Tailwind CSS 4 for modern styling
- Auth0 Next.js SDK for authentication
- ESLint (Next.js + custom config)
- Optimized images with Next.js Image component

### Goals

- ✅ Auth0 authentication with seamless login/logout flows
- ✅ Modern, responsive UI with professional navigation
- ✅ Dashboard with user statistics and quick actions
- ✅ Watchlist management interface (UI complete, backend integration ready)
- ✅ Search interface for content discovery
- ✅ User settings and profile management
- ✅ Streaming service subscription management UI
- ✅ User preferences and notification settings
- ✅ Responsive, accessible design across all devices

### Project Structure

```
app/
  api/
    me/
      route.ts          # User profile API proxy
    session/
      route.ts          # Session data API proxy
  dashboard/
    page.tsx            # Dashboard page (redirects to home)
  profile/
    page.tsx            # User profile and statistics
  search/
    page.tsx            # Content search interface
  settings/
    page.tsx            # User preferences and service management
  watchlist/
    page.tsx            # Watchlist management interface
  favicon.ico
  globals.css           # Tailwind base + custom styles
  layout.tsx            # Root layout with Auth0Provider
  loading.tsx           # Global loading component
  not-found.tsx         # 404 error page
  page.tsx              # Landing page / authenticated dashboard
components/
  Footer.tsx            # Site footer with links
  Navigation.tsx        # Main navigation with user menu
  ui.tsx                # Reusable UI components (Button, LoadingSpinner, etc.)
lib/
  auth0.ts              # Auth0 client configuration
public/
  default-avatar.svg    # Default user avatar
  (other static assets)
middleware.ts           # Auth0 middleware for route protection
```

### Getting Started

Install dependencies (choose one):

```bash
pnpm install
# or
npm install
# or
yarn install
```

Run dev server:

```bash
pnpm dev
```

Then open http://localhost:3000

### Environment Variables

Create `frontend/.env.local` with Auth0 and backend configuration:

```env
# Auth0 configuration for Next.js
AUTH0_SECRET='long-random-string-for-session-encryption'
AUTH0_DOMAIN='https://your-domain.auth0.com'
AUTH0_CLIENT_ID='your-auth0-client-id'
AUTH0_CLIENT_SECRET='your-auth0-client-secret'
AUTH0_AUDIENCE='https://api.watchlistnotify.com'
AUTH0_SCOPE='openid profile email'

# Backend API URL for server-side requests
BACKEND_URL='http://localhost:8080'
NEXT_PUBLIC_AUTH0_AUDIENCE='https://api.watchlistnotify.com'
```

### Auth0 Setup

1. Create a Regular Web Application in Auth0 Dashboard
2. Set Allowed Callback URLs: `http://localhost:3000/auth/callback`
3. Set Allowed Logout URLs: `http://localhost:3000`
4. Create a Custom API with audience `https://api.watchlistnotify.com`
5. Add Post-Login Action to include custom claims in access tokens

### Auth Flow (frontend perspective)

1. User clicks "Sign in" or "Get Started" -> redirects to `/auth/login`
2. Auth0 Universal Login handles authentication (Google OAuth, username/password, etc.)
3. Auth0 redirects to `/auth/callback` with authorization code
4. Auth0 Next.js SDK exchanges code for tokens and creates user session
5. Frontend can access user data via `useUser()` hook from `@auth0/nextjs-auth0`
6. Protected API calls to backend include Auth0 access token automatically

Implementation Notes:

- Uses Auth0 Next.js SDK for seamless integration
- Session data stored in encrypted cookies managed by Auth0 SDK
- User state available across components via `useUser()` hook
- Protected routes automatically redirect to login when needed
- Backend API calls include access token with custom claims

### Implemented Components

- ✅ **Navigation**: Responsive navbar with user menu, mobile support, and Auth0 integration
- ✅ **Footer**: Professional site footer with structured links
- ✅ **UI Components**: Reusable Button, LoadingSpinner, EmptyState components
- ✅ **Landing Page**: Beautiful hero section with features and call-to-action
- ✅ **Dashboard**: User statistics, quick actions, and activity overview
- ✅ **Search Interface**: Content search with filters and popular suggestions
- ✅ **Watchlist Management**: Clean interface for managing watchlist items
- ✅ **Settings Page**: User preferences, streaming services, and notification controls
- ✅ **Profile Page**: User information, statistics, and activity history
- ✅ **Loading States**: Global and component-level loading indicators
- ✅ **Error Handling**: Custom 404 page and error boundaries

### Planned Enhancements

- Backend integration for watchlist CRUD operations
- Real search functionality with TMDb API
- Streaming service availability indicators
- Email notification preferences UI
- Toast notification system
- Dark mode support
- Mobile app-like experience (PWA)

### Styling

- Modern design system with consistent color palette (blue/purple gradients)
- Tailwind CSS 4 utilities for rapid development
- Custom CSS for animations, scrollbars, and accessibility features
- Responsive design with mobile-first approach
- Professional typography using Geist fonts
- Smooth transitions and hover effects throughout
- Focus states for keyboard navigation accessibility

### Design System

- **Primary Colors**: Blue (#3b82f6) to Purple (#8b5cf6) gradients
- **Typography**: Geist Sans for UI, Geist Mono for code
- **Spacing**: Consistent 8px grid system
- **Components**: Rounded corners, subtle shadows, clean borders
- **Accessibility**: High contrast ratios, keyboard navigation, screen reader support

### Linting & Formatting

Run lint:

```bash
pnpm lint
```

(Formatter not yet configured; consider Prettier or Biome.)

### API Integration

Backend integration handled through:

1. **Direct API calls** from frontend components to Go backend
2. **Next.js API routes** as proxies for server-side requests (see `app/api/`)
3. **Auth0 access tokens** automatically included in requests to backend
4. **Error handling** with consistent error states and fallbacks

Example API call pattern:

```typescript
// Client-side component
const { data, isLoading, error } = useSWR("/api/me", fetcher);

// Server-side API route (app/api/me/route.ts)
export async function GET() {
  const { token } = await auth0.getAccessToken();
  const response = await fetch(`${process.env.BACKEND_URL}/api/me`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return Response.json(await response.json());
}
```

### Data Fetching Strategy

- **Public pages**: Static generation where possible, dynamic for personalized content
- **Authenticated pages**: Server Components with Auth0 token-based API calls
- **Client components**: Use Auth0 hooks (`useUser`) for user state
- **Protected routes**: Automatic redirect to Auth0 login via middleware
- **Error boundaries**: Graceful degradation with loading and error states

### Route Protection

```typescript
// middleware.ts handles Auth0 session management
export default auth0.middleware({
  matcher: ["/((?!api|_next/static|_next/image|favicon.ico).*)"],
});

// Components check auth state
const { user, isLoading } = useUser();
if (!user) return <LoginPrompt />;
```

### Accessibility

- Use semantic HTML and ARIA roles only when needed.
- Plan to add integration tests for keyboard navigation.

### Testing (planned)

- Unit: React Testing Library + Vitest / Jest (TBD)
- E2E: Playwright (for auth + flows)

### Deployment (Current & Planned)

**Current Production Setup:**

- **Frontend**: Deployed on Vercel at `https://watchlistnotify.com`
- **DNS/CDN**: Cloudflare manages DNS and provides edge caching/security
- **Auth0**: Production application configured with production callback URLs
- **Environment**: All Auth0 secrets configured in Vercel project settings

**Local Development:**

- Frontend runs on `http://localhost:3000`
- Backend API on `http://localhost:8080`
- Auth0 configured for local callback URLs
- MongoDB via Docker Compose

**Deployment Process:**

1. Push to main branch triggers automatic Vercel deployment
2. Vercel builds optimized Next.js bundle with Auth0 integration
3. Environment variables automatically applied from Vercel project settings
4. Cloudflare handles DNS routing and edge caching
5. Auth0 manages authentication flows in production

**Performance Optimizations:**

- Next.js Image optimization for user avatars and content posters
- Static generation for public pages
- Edge caching via Cloudflare for static assets
- Optimized bundle with tree-shaking and code splitting

### Roadmap (Frontend-Specific)

- [x] Auth0 integration with modern UI flows
- [x] Professional navigation and layout system
- [x] Complete user interface for all core features
- [x] Responsive design across all devices
- [x] Loading and error states standardized
- [x] User profile and settings management
- [ ] Backend integration for watchlist CRUD operations
- [ ] Real-time availability status indicators
- [ ] Email notification preferences with live preview
- [ ] Advanced search filters and sorting
- [ ] Dark mode theme support
- [ ] Progressive Web App (PWA) features
- [ ] Accessibility compliance (WCAG 2.1)
- [ ] Performance optimization and monitoring

### Contributing

Follow root project guidelines. Key principles:

- **Components**: Keep small, focused, and reusable
- **Accessibility**: Always include proper ARIA labels and keyboard navigation
- **Performance**: Use Next.js optimization features (Image, dynamic imports, etc.)
- **Styling**: Follow the established design system and Tailwind conventions
- **TypeScript**: Maintain strict type safety throughout
- **Testing**: Add tests for new components and user flows

### Development Tips

- Use `pnpm dev` for local development with hot reload
- Check responsive design at different breakpoints
- Test authentication flows in incognito/private browsing
- Validate accessibility with screen readers and keyboard navigation
- Monitor bundle size with `pnpm build` and analyze output

### Maintainer & Contact

Maintainer: Neil Mahajan  
Links & Site: https://links.neilsmahajan.com  
Personal Email: neilsmahajan@gmail.com  
Project Contact: contact@watchlistnotify.com

### License

MIT License – see root `LICENCE` file.

SPDX-License-Identifier: MIT

---

Questions / ideas? Open an issue in the main repo.
