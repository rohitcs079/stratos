import { async, ComponentFixture, TestBed, inject } from '@angular/core/testing';

import { InstancesTabComponent } from './instances-tab.component';
import { CoreModule } from '../../../../../../core/core.module';
import { SharedModule } from '../../../../../../shared/shared.module';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
import { StoreModule } from '@ngrx/store';
import { appReducers } from '../../../../../../store/reducers.module';
import { ApplicationService } from '../../../../application.service';
import { ApplicationServiceMock } from '../../../../../../test-framework/application-service-helper';
import { AppStoreModule } from '../../../../../../store/store.module';
import { ApplicationStateService } from '../../../../../../shared/components/application-state/application-state.service';
import { ApplicationEnvVarsService } from '../build-tab/application-env-vars.service';
import { RouterTestingModule } from '@angular/router/testing';
import { getInitialTestStoreState } from '../../../../../../test-framework/store-test-helper';
import { endpointStoreNames } from '../../../../../../store/types/endpoint.types';

describe('InstancesTabComponent', () => {
  let component: InstancesTabComponent;
  let fixture: ComponentFixture<InstancesTabComponent>;
  const initialState = getInitialTestStoreState();

  beforeEach(async(() => {
    TestBed.configureTestingModule({
      declarations: [InstancesTabComponent],
      imports: [
        CoreModule,
        SharedModule,
        RouterTestingModule,
        BrowserAnimationsModule,
        StoreModule.forRoot(
          appReducers,
          {
            initialState
          }
        ),
      ],
      providers: [
        { provide: ApplicationService, useClass: ApplicationServiceMock },
        AppStoreModule,
        ApplicationStateService,
        ApplicationEnvVarsService
      ]
    })
      .compileComponents();
  }));

  beforeEach(inject([ApplicationService], (applicationService: ApplicationService) => {
    const cfGuid = Object.keys(initialState.requestData[endpointStoreNames.type])[0];
    const appGuid = Object.keys(initialState.requestData.application)[0];
    fixture = TestBed.createComponent(InstancesTabComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  }));

  it('should create', () => {
    expect(component).toBeTruthy();
  });
});
