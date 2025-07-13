import { Navigate } from 'react-router';
import { useAuth } from '../contexts/AuthContext';

export function meta() {
  return [
    { title: 'Aquarium Fish' },
    { name: 'description', content: 'Aquarium Fish Dashboard' },
  ];
}

export default function Home() {
  const { isAuthenticated, isLoading } = useAuth();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto mb-4"></div>
          <p className="text-gray-600 dark:text-gray-300">Loading...</p>
        </div>
      </div>
    );
  }

  if (isAuthenticated) {
    return <Navigate to="/applications" replace />;
  }

  return <Navigate to="/login" replace />;
}
