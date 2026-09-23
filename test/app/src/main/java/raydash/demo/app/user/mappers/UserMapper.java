package raydash.demo.app.user.mappers;

import raydash.demo.app.user.dtos.response.UserDetailResponseDto;
import raydash.demo.app.user.models.UserEntity;

public final class UserMapper {
    private UserMapper() {}

    public static UserDetailResponseDto toUserDetailResponseDto(UserEntity user) {
        return new UserDetailResponseDto(
            user.getUserId(), 
            user.getName(), 
            user.getEmail()
        );
    }
}
