/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { lazy, Suspense, useContext, useMemo } from 'react';
import { Route, Routes, useLocation, useParams } from 'react-router-dom';
import Loading from './components/common/ui/Loading';
import User from './pages/User';
import { AuthRedirect, PrivateRoute, AdminRoute, RootRoute } from './helpers';
import RegisterForm from './components/auth/RegisterForm';
import LoginForm from './components/auth/LoginForm';
import NotFound from './pages/NotFound';
import Forbidden from './pages/Forbidden';
import Setting from './pages/Setting';
import { StatusContext } from './context/Status';

import PasswordResetForm from './components/auth/PasswordResetForm';
import PasswordResetConfirm from './components/auth/PasswordResetConfirm';
import Channel from './pages/Channel';
import Token from './pages/Token';
import Redemption from './pages/Redemption';
import TopUp from './pages/TopUp';
import Log from './pages/Log';
import Chat from './pages/Chat';
import Chat2Link from './pages/Chat2Link';
import Midjourney from './pages/Midjourney';
import Pricing from './pages/Pricing';
import Task from './pages/Task';
import ModelPage from './pages/Model';
import ModelDeploymentPage from './pages/ModelDeployment';
import Playground from './pages/Playground';
import Subscription from './pages/Subscription';
import OAuth2Callback from './components/auth/OAuth2Callback';
import PersonalSetting from './components/settings/PersonalSetting';
import Referral from './pages/Referral';
import AdminReferral from './pages/AdminReferral';
import ProviderPricing from './pages/ProviderPricing';
import RechargeAudit from './pages/RechargeAudit';
import Tickets from './pages/Tickets';
import Setup from './pages/Setup';
import SetupCheck from './components/layout/SetupCheck';
import SidebarModuleRoute from './components/layout/SidebarModuleRoute';

const Home = lazy(() => import('./pages/Home'));
const Dashboard = lazy(() => import('./pages/Dashboard'));
const About = lazy(() => import('./pages/About'));
const UserAgreement = lazy(() => import('./pages/UserAgreement'));
const PrivacyPolicy = lazy(() => import('./pages/PrivacyPolicy'));

function DynamicOAuth2Callback() {
  const { provider } = useParams();
  return <OAuth2Callback type={provider} />;
}

function App() {
  const location = useLocation();
  const [statusState] = useContext(StatusContext);

  // 获取模型广场权限配置
  const pricingRequireAuth = useMemo(() => {
    const headerNavModulesConfig = statusState?.status?.HeaderNavModules;
    if (headerNavModulesConfig) {
      try {
        const modules = JSON.parse(headerNavModulesConfig);

        // 处理向后兼容性：如果pricing是boolean，默认不需要登录
        if (typeof modules.pricing === 'boolean') {
          return false; // 默认不需要登录鉴权
        }

        // 如果是对象格式，使用requireAuth配置
        return modules.pricing?.requireAuth === true;
      } catch (error) {
        console.error('解析顶栏模块配置失败:', error);
        return false; // 默认不需要登录
      }
    }
    return false; // 默认不需要登录
  }, [statusState?.status?.HeaderNavModules]);

  return (
    <SetupCheck>
      <Routes>
        <Route
          path='/'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <Home />
            </Suspense>
          }
        />
        <Route
          path='/setup'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <Setup />
            </Suspense>
          }
        />
        <Route path='/forbidden' element={<Forbidden />} />
        <Route
          path='/console/models'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='models'>
                <ModelPage />
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/deployment'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='deployment'>
                <ModelDeploymentPage />
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/subscription'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='subscription'>
                <Subscription />
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/channel'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='channel'>
                <Channel />
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/token'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='console' module='token'>
                <Token />
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/playground'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='chat' module='playground'>
                <Playground />
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/redemption'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='redemption'>
                <Redemption />
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/user'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='user'>
                <User />
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/user/reset'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <PasswordResetConfirm />
            </Suspense>
          }
        />
        <Route
          path='/login'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <AuthRedirect>
                <LoginForm />
              </AuthRedirect>
            </Suspense>
          }
        />
        <Route
          path='/register'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <AuthRedirect>
                <RegisterForm />
              </AuthRedirect>
            </Suspense>
          }
        />
        <Route
          path='/sign-up'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <AuthRedirect>
                <RegisterForm />
              </AuthRedirect>
            </Suspense>
          }
        />
        <Route
          path='/reset'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <PasswordResetForm />
            </Suspense>
          }
        />
        <Route
          path='/oauth/github'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <OAuth2Callback type='github'></OAuth2Callback>
            </Suspense>
          }
        />
        <Route
          path='/oauth/discord'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <OAuth2Callback type='discord'></OAuth2Callback>
            </Suspense>
          }
        />
        <Route
          path='/oauth/oidc'
          element={
            <Suspense fallback={<Loading></Loading>}>
              <OAuth2Callback type='oidc'></OAuth2Callback>
            </Suspense>
          }
        />
        <Route
          path='/oauth/linuxdo'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <OAuth2Callback type='linuxdo'></OAuth2Callback>
            </Suspense>
          }
        />
        <Route
          path='/oauth/:provider'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <DynamicOAuth2Callback />
            </Suspense>
          }
        />
        <Route
          path='/console/setting'
          element={
            <RootRoute>
              <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                <Setting />
              </Suspense>
            </RootRoute>
          }
        />
        <Route
          path='/console/personal'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='personal' module='personal'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <PersonalSetting />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/topup'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='personal' module='topup'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <TopUp />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/referral'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='personal' module='referral'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <Referral />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/referral/:section'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='personal' module='referral'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <Referral />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/tickets'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='personal' module='tickets'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <Tickets />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/admin-referral'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='adminReferral'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <AdminReferral />
                </Suspense>
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/admin-tickets'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='ticket_management'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <Tickets adminMode />
                </Suspense>
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/admin-referral/:section'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='adminReferral'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <AdminReferral />
                </Suspense>
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/recharge-audit'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='recharge_audit'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <RechargeAudit />
                </Suspense>
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/provider-pricing'
          element={
            <AdminRoute>
              <SidebarModuleRoute section='admin' module='providerPricing'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <ProviderPricing />
                </Suspense>
              </SidebarModuleRoute>
            </AdminRoute>
          }
        />
        <Route
          path='/console/log'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='console' module='log'>
                <Log />
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='console' module='detail'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <Dashboard />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/midjourney'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='console' module='midjourney'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <Midjourney />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/console/task'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='console' module='task'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <Task />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        <Route
          path='/pricing'
          element={
            pricingRequireAuth ? (
              <PrivateRoute>
                <Suspense
                  fallback={<Loading></Loading>}
                  key={location.pathname}
                >
                  <Pricing />
                </Suspense>
              </PrivateRoute>
            ) : (
              <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                <Pricing />
              </Suspense>
            )
          }
        />
        <Route
          path='/about'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <About />
            </Suspense>
          }
        />
        <Route
          path='/user-agreement'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <UserAgreement />
            </Suspense>
          }
        />
        <Route
          path='/privacy-policy'
          element={
            <Suspense fallback={<Loading></Loading>} key={location.pathname}>
              <PrivacyPolicy />
            </Suspense>
          }
        />
        <Route
          path='/console/chat/:id?'
          element={
            <PrivateRoute>
              <SidebarModuleRoute section='chat' module='chat'>
                <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                  <Chat />
                </Suspense>
              </SidebarModuleRoute>
            </PrivateRoute>
          }
        />
        {/* 方便使用chat2link直接跳转聊天... */}
        <Route
          path='/chat2link'
          element={
            <PrivateRoute>
              <Suspense fallback={<Loading></Loading>} key={location.pathname}>
                <Chat2Link />
              </Suspense>
            </PrivateRoute>
          }
        />
        <Route path='*' element={<NotFound />} />
      </Routes>
    </SetupCheck>
  );
}

export default App;
