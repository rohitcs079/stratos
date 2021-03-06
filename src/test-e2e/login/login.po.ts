import { userInfo } from 'os';
import { E2EHelpers } from '../helpers/e2e-helpers';
import { element, by, browser, promise, protractor } from 'protractor';

const LOGIN_FAIL_MSG = 'Username and password combination incorrect. Please try again.';
const until = protractor.ExpectedConditions;

export class LoginPage {

  helpers = new E2EHelpers();

  navigateTo() {
    return browser.get('/login');
  }

  isLoginPage(): promise.Promise<boolean> {
    return browser.getCurrentUrl().then(url => url === browser.baseUrl + '/login');
  }

  getTitle() {
    return element(by.css('app-root h1')).getText();
  }

  enterLogin(username: string, password: string) {
    const formFields = this.helpers.getFormFields('loginForm');
    formFields.get(0).clear();
    formFields.get(1).clear();
    formFields.get(0).sendKeys(username);
    return formFields.get(1).sendKeys(password);
  }

  loginButton() {
    return this.helpers.getForm('loginForm').element(by.css('button[type="submit"]'));
  }

  getLoginError() {
    return element(by.css('.login-message.login-message--show.login-message-error')).getText();
  }

  login(username: string, password: string) {
    this.navigateTo();
    this.enterLogin(username, password);
    this.loginButton().click();
    // this.waitForApplicationPage();
  }

  waitForLoggedIn() {
    return browser.wait(until.presenceOf(element(by.tagName('app-dashboard-base'))), 5000);
  }

  isLoginError() {
    return this.getLoginError().then(text => text === LOGIN_FAIL_MSG);
  }

  // Wait until an application page is shown (one that uses the dashboard base)
  waitForApplicationPage() {
    return browser.wait(until.presenceOf(element(by.tagName('app-dashboard-base'))), 5000);
  }

  waitForLogin() {
    return browser.wait(until.presenceOf(element(by.tagName('app-login-page'))), 10000);
  }

  waitForNoEndpoints() {
    return browser.wait(until.presenceOf(element(by.tagName('app-no-endpoints-non-admin'))), 10000);
  }

}
