/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

import React, { useEffect, useRef } from 'react';

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  children: React.ReactNode;
  hasUnsavedChanges?: boolean;
  confirmMessage?: string;
  className?: string;
}

export const Modal: React.FC<ModalProps> = ({
  isOpen,
  onClose,
  children,
  hasUnsavedChanges = false,
  confirmMessage = 'You have unsaved changes. Are you sure you want to close?',
  className = 'bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-4xl max-h-[90vh] overflow-y-auto',
}) => {
  const contentRef = useRef<HTMLDivElement>(null);

  // Handle backdrop click
  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    // Only close if clicking the backdrop, not the content
    if (contentRef.current && !contentRef.current.contains(e.target as Node)) {
      handleClose();
    }
  };

  // Handle close with confirmation if needed
  const handleClose = () => {
    if (hasUnsavedChanges) {
      if (window.confirm(confirmMessage)) {
        onClose();
      }
    } else {
      onClose();
    }
  };

  // Handle escape key
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) {
        handleClose();
      }
    };

    if (isOpen) {
      document.addEventListener('keydown', handleEscape);
      return () => document.removeEventListener('keydown', handleEscape);
    }
  }, [isOpen, hasUnsavedChanges]);

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
      onClick={handleBackdropClick}
    >
      <div ref={contentRef} className={className}>
        {children}
      </div>
    </div>
  );
};
